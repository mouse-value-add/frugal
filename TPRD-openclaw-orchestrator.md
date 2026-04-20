# TPRD: Frugal as OpenClaw Orchestration Layer

- **Author:** Mouse
- **Date:** 2026-04-20
- **Status:** Draft (for implementation planning)

## 1) Problem
OpenClaw users need low-latency, low-cost handling for routine conversations and periodic reporting tasks, while still getting high-capability models for coding and complex reasoning. Today this can be assembled manually, but there is no opinionated orchestration blueprint that standardizes routing behavior, escalation criteria, and observability.

## 2) Goal
Define a production-ready orchestration design where **Frugal** acts as the model router for OpenClaw workloads:
- Route simple conversations and cron summaries to local, low-cost models (for example Gemma-class via local runtime).
- Escalate to premium models (for example Codex/Opus class) when intent and complexity require it.
- Keep quality high with explicit fallback rules, confidence thresholds, and auditability.

## 3) Non-Goals
- Building or shipping all implementation code in this PR.
- Forcing one local runtime vendor (Ollama/LM Studio/etc.).
- Replacing OpenClaw tools, skills, or ACP harness behavior.

## 4) Target Users
1. Solo builders running OpenClaw on personal hardware.
2. Teams who want predictable cost envelopes for autonomous workflows.
3. Operators running cron-heavy assistants with occasional high-stakes tasks.

## 5) User Stories
1. As a user, I want routine chat to stay local by default so latency and cost are minimal.
2. As a user, I want coding and deep reasoning tasks to auto-escalate to stronger models when needed.
3. As an operator, I want cron job summaries to be cheap and reliable, with escalation only on anomaly.
4. As an admin, I want deterministic routing logs to audit why a model was selected.

## 6) Proposed Solution

### 6.1 Routing Tiers
- **Tier L (Local):** Gemma-class local model for lightweight chat, summaries, status updates.
- **Tier M (Mid):** Cloud mid-tier model for ambiguous or medium-complexity prompts.
- **Tier H (High):** Premium model (Codex/Opus class) for coding, architecture, sensitive decisions, long-horizon reasoning.

### 6.2 Intent Classification
Frugal classifies each request with:
- Intent label (`smalltalk`, `status`, `summarization`, `coding`, `analysis`, `high-stakes`)
- Complexity score (0-1)
- Risk score (0-1)
- Confidence score (0-1)

Routing policy example:
- If intent in `{smalltalk,status,summarization}` and complexity < 0.45 and risk < 0.30 -> Tier L
- If coding intent OR complexity >= 0.70 -> Tier H
- If classifier confidence < threshold -> Tier M (or Tier H for risky intents)

### 6.3 Cron-Aware Behavior
For cron/system prompts:
- Default to Tier L for digest/report formatting.
- Promote to Tier M/H if failures, anomalies, or action recommendations exceed confidence threshold.
- Include machine-readable summary block for downstream automations.

### 6.4 Escalation & Fallback
- Upward fallback on quality failure, timeout, or policy mismatch.
- Hard cap on retries/fallback loops.
- Preserve user-visible continuity when switching model tiers.

### 6.5 Explainability
Attach structured routing metadata per request:
- selected tier/model
- classifier outputs
- fallback count
- latency/cost estimates
- short reason code (`simple_status_local`, `coding_high_tier`, etc.)

## 7) Functional Requirements
1. Configurable tier-to-model mapping.
2. Intent classification before final model selection.
3. Confidence/risk-based escalation rules.
4. Cron-mode policy with anomaly-based promotion.
5. Structured routing metadata available in logs and headers.
6. Retry + fallback guardrails (bounded attempts, timeout budgets).
7. Deterministic behavior under missing/invalid classifier output (safe defaults).

## 8) Non-Functional Requirements
- p95 routing overhead < 120 ms (excluding model inference).
- Zero unbounded fallback loops.
- Backward-compatible pass-through mode.
- Clear operator docs and example configs.

## 9) Risks & Mitigations
- **Misclassification risk:** Start conservative, route ambiguous intents upward.
- **Local model drift:** Add quality checks and promote on uncertainty.
- **Cost spikes from over-escalation:** daily budget ceilings + alerting.
- **Operational complexity:** ship reference policies and presets.

## 10) Success Metrics
- >=60% of eligible requests served on Tier L with no quality regression.
- <=5% manual reroute complaints.
- 25-50% cost reduction for mixed chat + cron workloads.
- p95 end-to-end latency improvement for routine conversations.

## 11) Rollout Plan
1. **Phase 0 (Spec):** finalize policy schema and reason codes.
2. **Phase 1 (Shadow):** classify + log decisions, do not enforce.
3. **Phase 2 (Partial):** enforce Tier L for low-risk intents, monitor.
4. **Phase 3 (Full):** enable full escalation/fallback policy with budget controls.

## 12) Open Questions
1. Should high-stakes intents always skip Tier M and go directly Tier H?
2. Should cron anomaly promotion use static thresholds or adaptive baselines?
3. What minimum eval set is required before enabling full enforcement by default?

## 13) Implementation Checklist (Future PRs)
- [ ] Add policy schema for tier mapping + thresholds.
- [ ] Add classifier output contract (intent/complexity/risk/confidence).
- [ ] Implement routing decision engine.
- [ ] Implement cron anomaly promotion path.
- [ ] Add reason-code logging/headers.
- [ ] Add routing eval harness and baseline dataset.
- [ ] Add docs with operator presets (local-first, balanced, quality-first).

