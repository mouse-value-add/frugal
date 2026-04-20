# TPRD: Frugal as OpenClaw Orchestration Layer

- **Author:** Mouse
- **Date:** 2026-04-20
- **Status:** Draft (for implementation planning)
- **Reviewer:** 

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


## 14) Monetization & Packaging Strategy

### Recommendation
Use a hybrid model, open-source core router plus paid managed and enterprise layers.

### Open Source (Free Core)
- Core routing engine (policy-based model selection)
- Intent classification contract
- Local-first support (for example Gemma-class via local runtime)
- Basic logs and reason codes
- Self-host deployment path

### Paid: Pro and Team Cloud
- Hosted control plane for policy management and rollouts
- Routing analytics dashboard (latency, quality proxy, cost by intent)
- Budget controls, anomaly alerts, and cron health reporting
- Managed evals and policy recommendations
- Team collaboration features (workspaces, shared presets)

### Paid: Enterprise
- RBAC, SSO/SAML, and granular audit logs
- Approval workflows and policy guardrails
- Private VPC/on-prem deployment options
- SLA, priority support, and onboarding
- Compliance support packages (as needed)

### Why this model
- OSS drives trust, adoption, and integrations in the OpenClaw ecosystem.
- Paid layers monetize operational complexity, governance, and reliability at scale.
- It keeps hobbyist entry friction low while creating clear enterprise upgrade paths.

## 15) Example Pricing (Draft)

- Free (OSS/self-host): $0, bring your own infra/models.
- Pro: $49-99/month per workspace, includes hosted control plane plus analytics and alerting.
- Team: $299-999/month, adds collaboration, advanced budgets, and deeper observability.
- Enterprise: custom annual contract (SLA, SSO, private deployment, support).

Pricing should be validated with 10-15 design partners before public launch.

## 16) OSS Marketing and Product Success Plan (Draft)

### Positioning
Frugal should be positioned as: **Open router for AI agents, local-first cost control with smart escalation**.

### Beachhead ICP
Start with one primary ICP:
- OpenClaw power users running multi-model agent workflows with recurring cron/reporting jobs.

### Product-Led Adoption
- Ship a 10-minute quickstart that demonstrates:
  1. Local model handles simple prompts.
  2. Complex prompts escalate automatically.
  3. Cost and latency impact are measurable immediately.
- Provide starter configs for common policy profiles:
  - local-first
  - balanced
  - quality-first

### Proof and Credibility
- Publish a reproducible benchmark suite comparing “with Frugal” vs “without Frugal” on identical workloads.
- Report at least: cost/request, p50/p95 latency, escalation rate, and quality proxy metrics.
- Keep benchmark scripts public and runnable to build trust.

### Distribution Loops
- “Built with Frugal” badge for downstream OSS projects.
- Weekly short case studies with before/after routing metrics.
- Integration templates for adjacent ecosystems (for example OpenClaw, LiteLLM, LangGraph-style workflows).

### Reliability as Differentiator
Prioritize operator trust features over surface-level feature count:
- deterministic reason codes for routing decisions
- robust logs/traces for each route/escalation
- bounded fallback behavior with explicit failure semantics

### Community Motions
- Curate “good first issue” and “routing policy recipe” issues.
- Hold monthly office hours and publish contributor highlights.
- Maintain fast maintainer response cadence for early community growth.

### 90-Day Execution Priorities
1. Positioning and docs refresh.
2. Benchmark harness plus public results page.
3. OpenClaw-focused integration guide and starter configs.
4. 2-5 design partners and quote-backed case studies.
5. Launch package: HN post, X thread, and short demo video.

### Success KPIs (90 days)
- GitHub stars and weekly active contributors.
- Number of active installs/workspaces.
- Share of requests handled by local tier with acceptable quality.
- Cost reduction achieved by pilot users.
- Time-to-first-value from install to first successful routed workflow.
