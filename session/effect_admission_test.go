package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ben-ranford/stave/effect"
	"github.com/ben-ranford/stave/event"
)

func TestEffectAdmissionReservationPublicationBoundary(t *testing.T) {
	q := newEffectAdmissionQueue(1)
	defer q.close()
	if err := q.reserve(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-q.committed:
		t.Fatal("unpublished reservation became available to the worker")
	default:
	}
	if err := q.reserve(); !errors.Is(err, ErrBackpressure) {
		t.Fatalf("second reservation = %v, want backpressure", err)
	}
	q.commit([]effect.Call{{Sequence: 1}})
	calls, ok := q.next(context.Background())
	if !ok || len(calls) != 1 || calls[0].Sequence != 1 {
		t.Fatalf("committed batch = %#v, %t", calls, ok)
	}
	if err := q.reserve(); !errors.Is(err, ErrBackpressure) {
		t.Fatalf("worker-held batch lost its reservation: %v", err)
	}
	q.release()
	if err := q.reserve(); err != nil {
		t.Fatalf("completed admission did not release capacity: %v", err)
	}
	q.commit([]effect.Call{{Sequence: 2}})
	calls, ok = q.next(context.Background())
	if !ok || len(calls) != 1 || calls[0].Sequence != 2 {
		t.Fatalf("next committed batch = %#v, %t", calls, ok)
	}
	q.release()
}

func TestEffectAdmissionCloseCancelsCommittedAndUnpublishedBatches(t *testing.T) {
	q := newEffectAdmissionQueue(2)
	for range 2 {
		if err := q.reserve(); err != nil {
			t.Fatal(err)
		}
	}
	q.commit([]effect.Call{{Sequence: 1}})
	q.close()
	q.close()
	q.commit([]effect.Call{{Sequence: 2}})
	q.release()
	if len(q.committed) != 0 || q.reserved != 0 {
		t.Fatalf("closed queue retained batches or reservations: %d, %d", len(q.committed), q.reserved)
	}
	if err := q.reserve(); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("closed queue reserve = %v", err)
	}
	if calls, ok := q.next(context.Background()); ok || calls != nil {
		t.Fatalf("closed queue produced %#v", calls)
	}
}

func TestEffectAdmissionWaitHonorsCancellation(t *testing.T) {
	q := newEffectAdmissionQueue(1)
	defer q.close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if calls, ok := q.next(ctx); ok || calls != nil {
		t.Fatalf("canceled wait produced %#v", calls)
	}
}

func TestSessionEffectsWaitForPublication(t *testing.T) {
	viewEntered := make(chan struct{})
	resumeView := make(chan struct{})
	started := make(chan struct{}, 1)
	observed := make(chan error, 1)
	var viewOnce sync.Once
	var s *Session[model]
	s = newSession(t, Options[model]{
		EffectPorts: map[string]effect.Port{"probe": effect.PortFunc(func(_ context.Context, call effect.Call) (any, error) {
			started <- struct{}{}
			snapshot, err := s.Snapshot()
			if err != nil {
				observed <- err
				return nil, err
			}
			transcript, err := s.Transcript()
			if err == nil && (snapshot.Sequence != call.Sequence || snapshot.Hashes.Declarations == "" || len(transcript.Records) != 1) {
				err = fmt.Errorf("unpublished effect: sequence=%d call=%d declarations=%q records=%d", snapshot.Sequence, call.Sequence, snapshot.Hashes.Declarations, len(transcript.Records))
			}
			if err == nil {
				record := transcript.Records[0]
				if record.Event.Kind != event.Key || record.Result.Sequence != call.Sequence || record.Result.Hashes.Declarations != snapshot.Hashes.Declarations {
					err = errors.New("producer transcript and published declaration disagree")
				}
			}
			observed <- err
			return nil, err
		})},
		Reduce: func(_ context.Context, m model, ev event.Event) (model, []effect.Request, error) {
			if ev.Kind != event.Key {
				return m, nil, nil
			}
			m.Count++
			return m, []effect.Request{{Spec: effect.Spec{Kind: "probe"}}}, nil
		},
		View: func(ctx context.Context, m model) (ViewResult, error) {
			if m.Count == 1 {
				viewOnce.Do(func() { close(viewEntered); <-resumeView })
			}
			return testView(ctx, m)
		},
	})
	defer s.Close()
	if err := s.Send(mustEvent(t, event.Key, event.KeyPayload{Key: "enter"})); err != nil {
		t.Fatal(err)
	}
	<-viewEntered
	// Let rendering finish while publication is deliberately unable to acquire
	// its lock. The queue test above proves the reservation boundary without a
	// timing assumption; this integration check exercises the actual worker.
	s.mu.Lock()
	close(resumeView)
	select {
	case <-started:
		sequence, records := s.current.Sequence, len(s.transcript.Records)
		s.mu.Unlock()
		t.Fatalf("effect started before publication: sequence=%d records=%d", sequence, records)
	case <-time.After(50 * time.Millisecond):
		s.mu.Unlock()
	}
	select {
	case err := <-observed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("published effect did not reach its port")
	}
}
