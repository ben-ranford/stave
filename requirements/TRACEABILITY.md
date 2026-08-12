# Stave v1.0.0 Traceability

`requirements/traceability.json` is the machine-readable source of truth. `implemented` means the current Stave repository or an explicitly identified proving-client worktree supplies concrete code and verification evidence. `planned` means the complete contract is not yet proven.

Lopper is Stave's first proving client, not a core product boundary. Atlas is the contrasting second-client and brand fixture; the Bubble Tea and Lip Gloss modules also consume one shared adapter-neutral surface fixture. Lopper proof references are published as portable `external://` citations until the proving-client worktree is mirrored into this repository.

Release validation has two explicit levels: `make release-contract` validates a
prerelease candidate, while `make release-ga-contract` rejects every planned
normative entry or unpublished external proof. The v1.0.0 GA gate remains
blocked while any P0, P1, or acceptance-criterion entry is planned.

## Functional requirements

| ID | Requirement | Verification evidence | Status | Milestone |
| --- | --- | --- | --- | --- |
| `FR-001` | Core model stays renderer/runtime neutral | `stave_test.go::TestProgramComposesProductionSession`<br>`go run ./scripts/rigor/cmd/rigor boundary-check` | implemented | M1 |
| `FR-002` | State transitions occur through typed actions or explicit events | `session/session_test.go::TestSessionSerializesReducerOwnership`<br>`session/session_test.go::TestSessionCheckpointAndReplayStayDeterministic` | implemented | M2 |
| `FR-003` | Actions expose stable IDs, schemas, validation, risk metadata, confirmation metadata, and typed results | `action/action_test.go::TestRegistryRejectsAndConfirmation`<br>`action/contract_freeze_test.go::TestNumericEnumExactEqualityInputAndOutput` | implemented | M1 |
| `FR-004` | Side effects stay in application-supplied ports with no hidden core I/O | `session/session_test.go::TestSessionDeliversEffectResultsInDeclarationOrder`<br>`effect/effect_test.go::TestExecutorDeliversDeclarationOrderDespiteParallelCompletion` | implemented | M2 |
| `FR-005` | State snapshots are serializable and versioned | `semantic/semantic_test.go::TestSnapshotNodeRoundTripAndRedaction`<br>`state/state_test.go::TestCheckpointRoundTrip` | implemented | M4 |
| `FR-010` | Snapshot exposes role, accessible name, value, state, relations, visibility, focusability, and stable ref | `semantic/semantic_test.go::TestSnapshotNodeRoundTripAndRedaction`<br>`runtime/agent/server_test.go::TestSnapshotEnvelopeTypedAndDiagnosticsIndependentOfActions` | implemented | M4 |
| `FR-011` | Stable refs disambiguate duplicate labels inside one snapshot/action cycle | `semantic/semantic_test.go::TestNodeIdentityStable`<br>`semantic/semantic_test.go::TestNodeIDGoldenVectors` | implemented | M1 |
| `FR-012` | Invalidated refs emit remap or divergence information | `replay/replay_test.go::TestValidateFailsClosedAtFirstEventMismatchWithRemap`<br>`replay/replay_test.go::TestValidateDetectsVersionAndHashDivergences` | implemented | M2 |
| `FR-013` | Every meaningful human action has keyboard and typed machine equivalents | `conformance/primitive_test.go::TestInteractivePrimitiveActionsHaveKeyboardBindings`<br>`keymap/keymap_test.go::TestDispatcherKeepsAllDefaultBindingsMapped` | implemented | M6 |
| `FR-014` | Agent control negotiates capabilities before action execution | `runtime/agent/server_test.go::TestServerRequiresInitialization`<br>`runtime/agent/server_test.go::TestServerNegotiationDoesNotEchoClientOffer` | implemented | M4 |
| `FR-015` | Structured action errors carry stable codes and safe context | `runtime/agent/server_test.go::TestServerInvokeUsesCamelCaseDTOAndSafeTypedError`<br>`event/event_test.go::TestSensitivePayloadsAreRedactedOnSerialization` | implemented | M4 |
| `FR-016` | JSONL and/or JSON-RPC over stdio is available for non-interactive control | `runtime/agent/server_test.go::TestServerHandshakeAndStdoutPurity`<br>`protocol/jsonl_test.go::TestDecodeLineRejectsDuplicateAndTrailing` | implemented | M4 |
| `FR-017` | Coordinate or canvas fallback is explicit, negotiated, and lower priority than semantic interaction | `capability/capability_test.go::TestStrictIntersectionAndDenials`<br>`capability/capability_test.go::TestContradictoryOverridesDegradeSafely` | implemented | M4 |
| `FR-020` | Theme resolves the required semantic role groups | `theme/theme_test.go::TestRoleResolutionAcrossTwoThemes`<br>`theme/theme_test.go::TestThemeMissingRequiredTokensFails` | implemented | M3 |
| `FR-021` | Primitives consume semantic roles rather than raw ramps or global theme state | `conformance/primitive_test.go::TestTwoBrandsShareSemanticContract`<br>`render/render_test.go::TestRenderConsumesStyleIntentAndStatusVariantsAcrossColorModes` | implemented | M3 |
| `FR-022` | Theme supports dark or light policy and capability overrides | `theme/theme_test.go::TestModeAndCapabilityOverrides`<br>`capability/capability_test.go::TestCapabilityProfileMatrix` | implemented | M3 |
| `FR-023` | Density is configurable without changing semantic structure | `theme/theme_test.go::TestThemeDTCGRoundTrip`<br>`conformance/primitive_test.go::TestTwoBrandsShareSemanticContract` | implemented | M3 |
| `FR-024` | Motion is tokenized and reduced-motion policy disables timing dependence | `theme/theme_test.go::TestReducedMotionZeroesDurations`<br>`render/render_test.go::TestRenderConsumesStyleIntentAndStatusVariantsAcrossColorModes` | implemented | M3 |
| `FR-025` | Marks, icons, banners, and ASCII fallbacks are pluggable assets | `theme/theme_test.go::TestAssetAndGlyphFallbackSelection`<br>`internal/examples/theme_test.go::TestIndependentBrandThemesAcrossColourLadder` | implemented | M3 |
| `FR-030` | Renderer accepts model, viewport, capabilities, theme, and event context explicitly | `render/render_test.go::TestRenderRequestIsExplicitAndDeterministic`<br>`layout/layout_test.go::TestMeasureAndArrangeAreDeterministic` | implemented | M3 |
| `FR-031` | Plain-text renderer exists as the reference CI-safe output | `render/render_test.go::TestMachineAndPlainOutputsAvoidANSIAndPreserveSemantics`<br>`go run ./cmd/atlas` | implemented | M4 |
| `FR-032` | Terminal renderer sanitizes untrusted control characters at the final write boundary | `render/render_test.go::TestWriterSanitizesHostileCellContentAndNarrowResize`<br>`internal/terminal/sanitize_test.go::FuzzSanitize` | implemented | M5 |
| `FR-033` | Primitive set includes the required foundational, collection, state, input, viewport, and frame primitives | `testfixture/catalog_test.go::TestPrimitiveManifestUsesRealConstructors`<br>`conformance/negative_test.go::TestPrimitiveRejectsInvalidBoundsAndAmbiguousTabs` | implemented | M6 |
| `FR-034` | Tables support sticky headers, numeric alignment, selection, and non-modal disclosure | `primitive/primitive_test.go::TestTableModalAndMasterDetailContracts`<br>`primitive/primitive_test.go::TestTableRejectsInvalidWindowAndSelectionContracts` | implemented | M6 |
| `FR-035` | Renderer supports deterministic narrow, no-colour, ASCII, and non-TTY degradation | `capability/capability_test.go::TestCapabilityProfileMatrix`<br>`render/render_test.go::TestMachineAndPlainOutputsAvoidANSIAndPreserveSemantics` | implemented | M3 |
| `FR-040` | Lopper integration lives outside core and preserves Lopper domain semantics | `external://github.com/ben-ranford/lopper@unpublished-feat-1492-stave-v2-production#internal/ui/stave_preview_test.go::TestStaveParityActualASCIIEqualsLegacy`<br>`go run ./scripts/rigor/cmd/rigor boundary-check` | planned | M7 |
| `FR-041` | Lopper adapter preserves command grammar and codemod or baseline confirmation behavior | `external://github.com/ben-ranford/lopper@unpublished-feat-1492-stave-v2-production#internal/ui/stave_preview_test.go::TestStavePreviewPreservesCommandAndConsequentialActionGrammar`<br>`external://github.com/ben-ranford/lopper@unpublished-feat-1492-stave-v2-production#internal/ui/summary_commands_test.go::TestSummaryCommandHandlersPagingAndShortcuts` | planned | M7 |
| `FR-042` | Lopper adapter supports existing snapshot and non-interactive paths | `external://github.com/ben-ranford/lopper@unpublished-feat-1492-stave-v2-production#internal/ui/stave_preview_test.go::TestStavePreviewSnapshotFileAndDisabledLegacy`<br>`external://github.com/ben-ranford/lopper@unpublished-feat-1492-stave-v2-production#internal/ui/stave_preview_test.go::TestStavePreviewDeterministicAndSanitized` | planned | M7 |
| `FR-043` | A contrasting non-Lopper fixture uses the core before compatibility claims | `make atlas-check`<br>`cmd/lopper/main_test.go::TestLopperFixtureUsesIndependentTerminalProfile`<br>`internal/atlasrig/atlasrig_test.go::TestProofMatrixIsCompleteDeterministicAndProfileHonest`<br>`internal/atlasrig/atlasrig_test.go::TestProgramOwnsStateAndTranscriptReplays`<br>`internal/atlasrig/atlasrig_test.go::TestTypedActionsValidateAndConfirmationIsSingleUse`<br>`scripts/rigor/generated/atlas.render.txt`<br>`scripts/rigor/generated/atlas.matrix.json`<br>`internal/examples/theme_test.go::TestIndependentBrandThemesAcrossColourLadder` | implemented | M7 |

## Non-functional requirements

| ID | Requirement | Verification evidence | Status | Milestone |
| --- | --- | --- | --- | --- |
| `NFR-001` | Determinism | `session/session_test.go::TestSessionCheckpointAndReplayStayDeterministic`<br>`layout/layout_test.go::TestMeasureAndArrangeAreDeterministic` | implemented | M2 |
| `NFR-002` | Testability | `go test ./...`<br>`testfixture/catalog_test.go::TestPrimitiveManifestUsesRealConstructors` | implemented | M2 |
| `NFR-003` | Compatibility | `make api-boundary`<br>`scripts/rigor/generated/public-api.txt` | implemented | M8 |
| `NFR-004` | Performance | `make verify-performance`<br>`layout/layout_test.go::TestArrangeCancelsAndEnforcesBudgets`<br>`performance/performance_test.go::TestFixtureHasRequestedNodeCountAndStableHash` | implemented | M8 |
| `NFR-005` | Accessibility | `primitive/primitive_test.go::TestHiddenAndDisabledPrimitivesCannotRemainFocusable`<br>`conformance/hostile_test.go::TestValidateTreeRejectsHiddenFocusableAndMissingFieldErrorRelation` | implemented | M6 |
| `NFR-006` | Security | `action/action_test.go::TestConcurrentConfirmationSingleUse`<br>`runtime/agent/server_test.go::TestServerInvokeUsesCamelCaseDTOAndSafeTypedError`<br>`internal/terminal/sanitize_test.go::FuzzSanitize` | implemented | M8 |
| `NFR-007` | Degradation | `capability/capability_test.go::TestCapabilityProfileMatrix`<br>`render/render_test.go::TestMachineAndPlainOutputsAvoidANSIAndPreserveSemantics` | implemented | M3 |
| `NFR-008` | Observability | `session/session_test.go::TestSessionViewFailureRejectsLateMutationAndPublishesDiagnostic`<br>`runtime/agent/server_test.go::TestSnapshotEnvelopeTypedAndDiagnosticsIndependentOfActions` | implemented | M8 |
| `NFR-009` | Portability | `make adapters`<br>`capability/capability_test.go::TestCrossPlatformTerminalColourDetection`<br>`.github/workflows/ci.yml` | implemented | M5 |
| `NFR-010` | Maintainability | `requirements/traceability_test.go::TestTraceabilityMatrix`<br>`testfixture/catalog_test.go::TestPrimitiveManifestUsesRealConstructors`<br>`primitive/CONTRACT.md` | implemented | M8 |

## Primitive coverage

| ID | Requirement | Verification evidence | Status | Milestone |
| --- | --- | --- | --- | --- |
| `PRIMITIVE-P0-TEXT-LABEL` | P0 text or label primitive | `testfixture/catalog_test.go::TestPrimitiveManifestUsesRealConstructors`<br>`semantic/semantic_test.go::TestSnapshotNodeRoundTripAndRedaction` | implemented | M6 |
| `PRIMITIVE-P0-STACK-ROW-GRID` | P0 stack, row, and grid primitives | `testfixture/catalog_test.go::TestPrimitiveManifestUsesRealConstructors`<br>`layout/layout_test.go::TestArrangeSupportsStackRowSplitGridScrollOverlayInsetConditionalAndRecords` | implemented | M6 |
| `PRIMITIVE-P0-SURFACE-FRAME` | P0 surface or frame primitive | `testfixture/catalog_test.go::TestPrimitiveManifestUsesRealConstructors`<br>`surface/surface_test.go::TestPatchRoundTripAndDirtyRegions` | implemented | M6 |
| `PRIMITIVE-P0-TABLE-LIST` | P0 table or list primitive | `conformance/primitive_test.go::TestTableContractAndStableRowIDs`<br>`primitive/primitive_test.go::TestTableExplicitStates` | implemented | M6 |
| `PRIMITIVE-P0-STATUS-CHIP` | P0 status or chip primitive | `testfixture/catalog_test.go::TestPrimitiveManifestUsesRealConstructors`<br>`render/render_test.go::TestRenderConsumesStyleIntentAndStatusVariantsAcrossColorModes` | implemented | M6 |
| `PRIMITIVE-P0-FOCUS-INDICATOR` | P0 focus indicator primitive | `testfixture/catalog_test.go::TestPrimitiveManifestUsesRealConstructors`<br>`focus/focus_test.go::TestGraphScopePushAndPopRestoresPreviousFocus` | implemented | M6 |
| `PRIMITIVE-P0-DISCLOSURE` | P0 disclosure primitive | `testfixture/catalog_test.go::TestPrimitiveManifestUsesRealConstructors`<br>`primitive/primitive_test.go::TestTableModalAndMasterDetailContracts` | implemented | M6 |
| `PRIMITIVE-P0-VIEWPORT` | P0 viewport primitive | `testfixture/catalog_test.go::TestPrimitiveManifestUsesRealConstructors`<br>`layout/layout_test.go::TestArrangeOmitsHiddenOffscreenAndZeroAreaFocusableNodes` | implemented | M6 |
| `PRIMITIVE-P0-INPUT` | P0 input primitive including secure input | `primitive/primitive_test.go::TestFieldPrimitivesExposeErrorRelations`<br>`input/input_test.go::TestMemorySecretStoreOnlyExposesOpaqueHandle` | implemented | M6 |
| `PRIMITIVE-P0-EMPTY-LOADING-ERROR` | P0 empty, loading, and error state primitives | `primitive/primitive_test.go::TestTableExplicitStates`<br>`testfixture/catalog_test.go::TestPrimitiveManifestUsesRealConstructors` | implemented | M6 |
| `PRIMITIVE-P0-TERMINAL-WRITER` | P0 terminal writer primitive | `internal/terminal/sanitize_test.go::TestWriterSanitizesFinalSink`<br>`render/render_test.go::TestWriterExactANSISequenceFixture` | implemented | M5 |
| `PRIMITIVE-P0-SEMANTIC-SNAPSHOT-NODE` | P0 semantic snapshot node primitive | `semantic/semantic_test.go::TestSnapshotNodeRoundTripAndRedaction`<br>`semantic/semantic_test.go::TestTreeSnapshotRedaction` | implemented | M1 |
| `PRIMITIVE-P0-ACTION-DESCRIPTOR` | P0 action descriptor primitive | `action/contract_freeze_test.go::TestRegistryDefinitionDeepImmutability`<br>`keymap/keymap_test.go::TestInventoryIsImmutableAndExposesVersionedActions` | implemented | M1 |
| `PRIMITIVE-P0-DIAGNOSTICS` | P0 diagnostics primitive | `session/session_test.go::TestSessionViewFailureRejectsLateMutationAndPublishesDiagnostic`<br>`runtime/agent/server_test.go::TestSnapshotEnvelopeTypedAndDiagnosticsIndependentOfActions` | implemented | M4 |
| `PRIMITIVE-P1-TABS` | P1 tabs primitive | `testfixture/catalog_test.go::TestPrimitiveManifestUsesRealConstructors`<br>`conformance/negative_test.go::TestPrimitiveRejectsInvalidBoundsAndAmbiguousTabs` | implemented | M6 |
| `PRIMITIVE-P1-PAGINATION` | P1 pagination primitive | `testfixture/catalog_test.go::TestPrimitiveManifestUsesRealConstructors`<br>`primitive/primitive_test.go::TestTableRejectsInvalidWindowAndSelectionContracts` | implemented | M6 |
| `PRIMITIVE-P1-COMMAND-PALETTE` | P1 command palette primitive | `testfixture/catalog_test.go::TestPrimitiveManifestUsesRealConstructors`<br>`conformance/primitive_test.go::TestInteractivePrimitiveActionsHaveKeyboardBindings` | implemented | M6 |
| `PRIMITIVE-P1-CHART-PRIMITIVES` | P1 chart primitives | `testfixture/catalog_test.go::TestPrimitiveManifestUsesRealConstructors`<br>`render/render_test.go::TestMachineAndPlainOutputsAvoidANSIAndPreserveSemantics` | implemented | M6 |
| `PRIMITIVE-P1-INSPECTOR-LAYOUT-SLOT` | P1 inspector layout slot | `testfixture/catalog_test.go::TestPrimitiveManifestUsesRealConstructors`<br>`primitive/primitive_test.go::TestTableModalAndMasterDetailContracts` | implemented | M6 |
| `PRIMITIVE-P1-MODAL-CONFIRMATION-SHELL` | P1 modal or confirmation shell | `testfixture/catalog_test.go::TestPrimitiveManifestUsesRealConstructors`<br>`primitive/primitive_test.go::TestTableModalAndMasterDetailContracts`<br>`focus/focus_test.go::TestGraphScopePushAndPopRestoresPreviousFocus` | implemented | M6 |
| `PRIMITIVE-P1-SSH-TRANSPORT-ADAPTER` | P1 SSH transport adapter | `adapters/ssh/bridge_test.go::TestSSHTransportContract_MultiClientIsolation`<br>`adapters/ssh/bridge_test.go::TestSSHTransportContract_ProfileNegotiationAndNoninteractiveFallback` | implemented | M5 |
| `PRIMITIVE-P1-BUBBLE-TEA-LIP-GLOSS-ADAPTERS` | P1 Bubble Tea or Lip Gloss adapters | `adapters/bubbletea/adapter_test.go::TestSharedAdapterSurfaceFixture`<br>`adapters/lipgloss/lipgloss_test.go::TestSharedAdapterSurfaceFixture` | implemented | M5 |
| `PRIMITIVE-P1-RICHER-ACCESSIBILITY-AGENT-TRANSPORTS` | P1 richer accessibility or agent transports | `runtime/agent/server_test.go::TestServerHandshakeAndStdoutPurity`<br>`runtime/agent/server_test.go::TestServerCancellationReservedLaneWhileInvokeBlocks`<br>`adapters/ssh/bridge_test.go::TestSSHTransportContract_NoninteractiveFallback` | implemented | M4 |

## Architecture invariants

| ID | Requirement | Verification evidence | Status | Milestone |
| --- | --- | --- | --- | --- |
| `AI-01` | Brand ownership | `go run ./cmd/lopper`<br>`make atlas-check`<br>`cmd/lopper/main_test.go::TestLopperFixtureUsesIndependentTerminalProfile`<br>`internal/atlasrig/atlasrig_test.go::TestProofMatrixIsCompleteDeterministicAndProfileHonest`<br>`scripts/rigor/generated/lopper.render.txt`<br>`scripts/rigor/generated/atlas.render.txt`<br>`scripts/rigor/generated/atlas.matrix.json` | implemented | M3 |
| `AI-02` | Reusable core | `go run ./scripts/rigor/cmd/rigor boundary-check`<br>`scripts/rigor/generated/dependency-inventory.json` | implemented | M1 |
| `AI-03` | Adapter boundary | `go run ./scripts/rigor/cmd/rigor boundary-check`<br>`external://github.com/ben-ranford/lopper@unpublished-feat-1492-stave-v2-production#internal/ui/stave_preview_test.go::TestStaveParityActualUnicodeEqualsLegacyAndPreservesUnicode` | planned | M7 |
| `AI-04` | Determinism | `session/session_test.go::TestSessionCheckpointAndReplayStayDeterministic`<br>`surface/surface_test.go::TestSurfaceDeterminismAndMergeDirty` | implemented | M2 |
| `AI-05` | Headless testability | `go test ./...`<br>`render/render_test.go::TestMachineAndPlainOutputsAvoidANSIAndPreserveSemantics` | implemented | M2 |
| `AI-06` | Human and agent parity | `conformance/primitive_test.go::TestInteractivePrimitiveActionsHaveKeyboardBindings`<br>`keymap/keymap_test.go::TestDispatcherRoutesActivateToTypedAction` | implemented | M6 |
| `AI-07` | Semantic machine interface | `runtime/agent/server_test.go::TestSnapshotEnvelopeTypedAndDiagnosticsIndependentOfActions`<br>`runtime/agent/server_test.go::TestServerInvokeUsesCamelCaseDTOAndSafeTypedError` | implemented | M4 |
| `AI-08` | Adaptive degradation | `capability/capability_test.go::TestCapabilityProfileMatrix`<br>`render/render_test.go::TestMachineAndPlainOutputsAvoidANSIAndPreserveSemantics` | implemented | M3 |
| `AI-09` | Replaceable edges | `make adapters`<br>`go run ./scripts/rigor/cmd/rigor boundary-check` | implemented | M5 |
| `AI-10` | Go API discipline | `make api-boundary`<br>`scripts/rigor/generated/public-api.txt` | implemented | M1 |
| `AI-11` | Immutable semantics | `semantic/semantic_test.go::TestTreeValidationAndPatch`<br>`action/contract_freeze_test.go::TestRegistryDefinitionAccessorClonesConcurrently` | implemented | M1 |
| `AI-12` | Effect isolation | `session/session_test.go::TestSessionDeliversEffectResultsInDeclarationOrder`<br>`effect/effect_test.go::TestExecutorDeliversDeclarationOrderDespiteParallelCompletion` | implemented | M2 |
| `AI-13` | Trusted control plane | `runtime/agent/server_test.go::TestServerRequiresInitialization`<br>`action/action_test.go::TestConcurrentConfirmationSingleUse` | implemented | M4 |
| `AI-14` | Secret exclusion | `event/event_test.go::TestSensitivePayloadsAreRedactedOnSerialization`<br>`state/state_test.go::TestCheckpointSanitizeExcludesSecrets`<br>`secret/secret_test.go::TestMemoryStoreOpaqueSingleUseAndDestroy` | implemented | M4 |
| `AI-15` | Final-sink safety | `internal/terminal/sanitize_test.go::TestWriterSanitizesFinalSink`<br>`internal/terminal/sanitize_test.go::FuzzSanitize` | implemented | M5 |

## Milestones

| ID | Requirement | Verification evidence | Status | Milestone |
| --- | --- | --- | --- | --- |
| `M0` | Contracts and spike harness | `requirements/traceability_test.go::TestTraceabilityMatrix`<br>`semantic/semantic_test.go::TestNodeIDGoldenVectors`<br>`docs/adr/README.md` | implemented | M0 |
| `M1` | Semantic and action core | `semantic/semantic_test.go::TestSnapshotNodeRoundTripAndRedaction`<br>`action/contract_freeze_test.go::TestNumericEnumExactEqualityInputAndOutput` | implemented | M1 |
| `M2` | Reducer, session, and replay | `session/session_test.go::TestSessionCheckpointAndReplayStayDeterministic`<br>`effect/effect_test.go::TestExecutorDeliversDeclarationOrderDespiteParallelCompletion` | implemented | M2 |
| `M3` | Capabilities, theme, layout, and surface | `capability/capability_test.go::TestCapabilityProfileMatrix`<br>`theme/theme_test.go::TestSemanticContrastAcrossColourLadder`<br>`layout/layout_test.go::TestMeasureAndArrangeAreDeterministic` | implemented | M3 |
| `M4` | Headless and agent runtime | `runtime/agent/server_test.go::TestServerHandshakeAndStdoutPurity`<br>`runtime/agent/server_test.go::TestServerInvokeUsesCamelCaseDTOAndSafeTypedError` | implemented | M4 |
| `M5` | Human runtime and optional adapters | `runtime/human/human_test.go::TestRuntimeSignalAndPanicRestore`<br>`make adapters` | implemented | M5 |
| `M6` | Primitives | `testfixture/catalog_test.go::TestPrimitiveManifestUsesRealConstructors`<br>`conformance/primitive_test.go::TestInteractivePrimitiveActionsHaveKeyboardBindings` | implemented | M6 |
| `M7` | Lopper adoption | `external://github.com/ben-ranford/lopper@unpublished-feat-1492-stave-v2-production#internal/ui/stave_preview_test.go::TestStaveParityActualASCIIEqualsLegacy`<br>`external://github.com/ben-ranford/lopper@unpublished-feat-1492-stave-v2-production#internal/ui/stave_preview_test.go::TestStavePreviewPreservesCommandAndConsequentialActionGrammar`<br>`go run ./cmd/atlas` | planned | M7 |
| `M8` | Hardening and v1 | `make fmt-check vet test race fuzz-smoke benchmark-smoke verify-performance license-inventory govulncheck`<br>`make release-contract`<br>`make release-ga-contract`<br>`make release-dry-run` | implemented | M8 |

## Acceptance criteria

| ID | Requirement | Verification evidence | Status | Milestone |
| --- | --- | --- | --- | --- |
| `AC-01` | Root public API contains no Bubble Tea, Lip Gloss, Bubbles, tcell, or Lopper type | `make api-boundary`<br>`scripts/rigor/generated/public-api.txt` | implemented | M1 |
| `AC-02` | Root dependency graph excludes Lopper and optional adapter dependencies | `go run ./scripts/rigor/cmd/rigor boundary-check`<br>`scripts/rigor/generated/dependency-inventory.json` | implemented | M1 |
| `AC-03` | Themes produce visibly distinct output without changing primitives | `go run ./cmd/lopper`<br>`make atlas-check`<br>`cmd/lopper/main_test.go::TestLopperFixtureUsesIndependentTerminalProfile`<br>`internal/atlasrig/atlasrig_test.go::TestProofMatrixIsCompleteDeterministicAndProfileHonest`<br>`scripts/rigor/generated/lopper.render.txt`<br>`scripts/rigor/generated/atlas.render.txt`<br>`scripts/rigor/generated/atlas.matrix.json` | implemented | M3 |
| `AC-04` | Reordering keyed rows preserves NodeIDs | `conformance/primitive_test.go::TestTableContractAndStableRowIDs`<br>`semantic/semantic_test.go::TestNodeIdentityStable` | implemented | M1 |
| `AC-05` | Replacing a node changes generation and rejects stale actions | `replay/replay_test.go::TestValidateFailsClosedAtFirstEventMismatchWithRemap`<br>`stave_test.go::TestProgramRejectsUnregisteredSemanticActions` | implemented | M2 |
| `AC-06` | Same replay inputs produce identical revision, tree, and surface hashes | `session/session_test.go::TestSessionCheckpointAndReplayStayDeterministic`<br>`surface/surface_test.go::TestSurfaceDeterminismAndMergeDirty` | implemented | M2 |
| `AC-07` | Reducer, view, layout, and headless render pass without a TTY | `render/render_test.go::TestMachineAndPlainOutputsAvoidANSIAndPreserveSemantics`<br>`go test ./session ./layout ./render` | implemented | M4 |
| `AC-08` | Every interactive primitive has accessible role, name, action, and focus semantics | `conformance/primitive_test.go::TestInteractivePrimitiveActionsHaveKeyboardBindings`<br>`conformance/hostile_test.go::TestValidateTreeRejectsHiddenFocusableAndMissingFieldErrorRelation` | implemented | M6 |
| `AC-09` | Every human command has a typed machine equivalent | `keymap/keymap_test.go::TestDispatcherKeepsAllDefaultBindingsMapped`<br>`conformance/primitive_test.go::TestInteractivePrimitiveActionsHaveKeyboardBindings` | implemented | M6 |
| `AC-10` | No-colour, ASCII, narrow, non-TTY, CI, reduced-motion, and accessible-line profiles retain complete meaning | `capability/capability_test.go::TestCapabilityProfileMatrix`<br>`render/render_test.go::TestMachineAndPlainOutputsAvoidANSIAndPreserveSemantics`<br>`theme/theme_test.go::TestReducedMotionZeroesDurations` | implemented | M5 |
| `AC-11` | Machine output contains no ANSI, banners, prompts, or progress decoration | `runtime/agent/server_test.go::TestServerHandshakeAndStdoutPurity`<br>`render/render_test.go::TestMachineAndPlainOutputsAvoidANSIAndPreserveSemantics` | implemented | M4 |
| `AC-12` | Prompt-injection strings remain inert content | `internal/terminal/sanitize_test.go::FuzzSanitize`<br>`conformance/negative_test.go::FuzzPrimitiveTextControlBytes` | implemented | M4 |
| `AC-13` | Secret values do not appear in snapshots, protocol, logs, diagnostics, replay, or surfaces | `event/event_test.go::TestSensitivePayloadsAreRedactedOnSerialization`<br>`state/state_test.go::TestCheckpointSanitizeExcludesSecrets`<br>`adapters/ssh/bridge_test.go::TestSSHTransportContract_NoSecretDiagnosticLeakage` | implemented | M4 |
| `AC-14` | Consequential actions require valid policy or confirmation and reject replay | `action/action_test.go::TestConfirmationWhitespaceAndReplay`<br>`action/action_test.go::TestConcurrentConfirmationSingleUse` | implemented | M4 |
| `AC-15` | Queue saturation is bounded and observable | `runtime/human/human_test.go::TestQueueCoalescesResizeAndDropsOnlyWhenFull`<br>`session/session_test.go::TestSessionQueueBackpressureAndCoalescing` | implemented | M5 |
| `AC-16` | Cancellation restores the terminal and prevents late mutation | `runtime/human/human_test.go::TestRuntimeRestoresOnEOFAndClosesOnce`<br>`session/session_test.go::TestSessionCancellationDiscardsLateEffectResults`<br>`go test -race ./...` | implemented | M5 |
| `AC-17` | Lopper Start or Snapshot flows, commands, TTY refresh, sanitization, baseline, and codemod safety stay covered | `external://github.com/ben-ranford/lopper@unpublished-feat-1492-stave-v2-production#internal/ui/stave_preview_test.go::TestStavePreviewPreservesCommandAndConsequentialActionGrammar`<br>`external://github.com/ben-ranford/lopper@unpublished-feat-1492-stave-v2-production#internal/ui/stave_preview_test.go::TestStavePreviewDeterministicAndSanitized`<br>`external://github.com/ben-ranford/lopper@unpublished-feat-1492-stave-v2-production#internal/ui/summary_commands_test.go::TestSummaryHelperBranches` | planned | M7 |
| `AC-18` | Lopper adapter imports Stave and Stave never imports Lopper | `go run ./scripts/rigor/cmd/rigor boundary-check`<br>`external://github.com/ben-ranford/lopper@unpublished-feat-1492-stave-v2-production#go.mod`<br>`external://github.com/ben-ranford/lopper@unpublished-feat-1492-stave-v2-production#internal/ui/stave_preview.go` | planned | M7 |
| `AC-19` | Bubble Tea and Lip Gloss adapters pass the same semantic or surface fixtures | `adapters/bubbletea/adapter_test.go::TestSharedAdapterSurfaceFixture`<br>`adapters/lipgloss/lipgloss_test.go::TestSharedAdapterSurfaceFixture`<br>`testfixture/surface.go` | implemented | M5 |
| `AC-20` | Formatting, vet, tests, race, fuzz smoke, vulnerability, licence, and benchmark gates pass fresh | `make fmt-check vet test race fuzz-smoke benchmark-smoke license-inventory govulncheck`<br>`.github/workflows/ci.yml` | implemented | M8 |
| `AC-21` | All proposed performance targets are measured or accepted via ADR | `make verify-performance`<br>`scripts/rigor/refresh-generated.sh performance-report`<br>`adapters/ssh/bridge_test.go::TestSSHTransportPerformanceBudget`<br>`docs/adr/ADR-017.md` | implemented | M8 |
| `AC-22` | Rollback to the legacy Lopper UI remains possible during pilot | `external://github.com/ben-ranford/lopper@unpublished-feat-1492-stave-v2-production#internal/cli/parse_tui_test.go::TestParseArgsTUIStavePreviewRequiresExplicitEnablement`<br>`external://github.com/ben-ranford/lopper@unpublished-feat-1492-stave-v2-production#internal/ui/stave_preview_test.go::TestStavePreviewSnapshotFileAndDisabledLegacy`<br>`docs/lopper-migration.md` | planned | M7 |

## Current v1.0.0 status

The release-candidate contract is implemented. Eight entries remain planned for
GA: `FR-040`, `FR-041`, `FR-042`, `AI-03`, `M7`, `AC-17`, `AC-18`, and
`AC-22`. They require published, immutable Lopper proving-client integration,
parity, and rollback evidence. Candidate publication also requires reviewed
remote CI for the exact commit being tagged.
