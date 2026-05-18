package recipe

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ToolCaller dispatches an MCP-style tool call. In production it's backed
// by an in-process MCP client session (the "loopback" dogfood pattern that
// keeps CLI and agent traffic on the same code path); in tests a fake.
type ToolCaller interface {
	CallTool(ctx context.Context, name string, args map[string]any) (ToolResult, error)
}

// ToolResult mirrors the relevant bits of an MCP tools/call response.
// Structured is the typed-output JSON (search.SearchOutput etc); Content
// is a text-only fallback for legacy clients (we use Structured first).
type ToolResult struct {
	Structured map[string]any
	Content    string
}

// ChatCaller dispatches a chat-completion. The implementation is responsible
// for model resolution (`auto` → routed pick) and provider dispatch. Cost
// is the actual USD paid for this call (provider usage * cost_per_1k).
type ChatCaller interface {
	// Resolve returns the model the router would pick for this request
	// without making an upstream call. Used by Plan() to preview routing.
	Resolve(req ChatRequest) string
	// Chat runs the chat completion and returns the response.
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
}

// ChatRequest is the recipe-executor view of a chat call — fewer knobs
// than the OpenAI-flavored ChatCompletionRequest because recipes don't
// expose tools, streaming, or fallback chains (those decisions belong to
// the underlying executor wiring).
type ChatRequest struct {
	// Model is "auto" to defer to the router, or a pinned model name.
	Model string
	// Quality is the recipe tier ("high" | "balanced" | "cost"). Only
	// consulted when Model is "auto".
	Quality string
	System  string
	Input   string
}

// ChatResponse carries the response text plus the executor's view of cost.
type ChatResponse struct {
	Model     string
	Output    string
	CostUSD   float64
	LatencyMS int64
}

// Executor runs a Recipe end-to-end. Tools and Chat are pluggable so the
// CLI can supply real MCP-loopback / model-router implementations while
// tests inject fakes.
type Executor struct {
	Tools ToolCaller
	Chat  ChatCaller
}

// NewExecutor builds an Executor. Both Tools and Chat must be non-nil; a
// recipe with no tool steps still benefits from the chat caller and
// vice-versa, but the structural assertion is "if you can't both, you
// can't run any starter recipe".
func NewExecutor(tools ToolCaller, chat ChatCaller) *Executor {
	return &Executor{Tools: tools, Chat: chat}
}

// Execution is the result of a successful Execute call. FinalOutput is the
// last chat step's text (recipes that end on a tool step report the
// final tool's Content string).
type Execution struct {
	RecipeID       string
	Tier           string
	Task           string
	Steps          []StepOutcome
	FinalOutput    string
	TotalCostUSD   float64
	TotalLatencyMS int64
}

// StepOutcome is the per-step observability footer the executor surfaces
// up to the CLI. Provider is the resolved search-provider for tool steps
// (e.g. "tavily") or "" for chat steps; Name is the tool name for tool
// steps or the resolved model name for chat steps.
type StepOutcome struct {
	Index      int
	Kind       string // "tool" | "chat"
	Name       string
	Provider   string
	OutputText string
	CostUSD    float64
	LatencyMS  int64
}

// Plan is the dry-run result of Plan(). Plan never makes network calls —
// it walks the recipe and reports what *would* run, with model picks
// resolved via the router and resolved-arg previews for tool steps.
type Plan struct {
	RecipeID string
	Tier     string
	Task     string
	Steps    []PlanStep
	Reason   string
}

// PlanStep mirrors one step in dry-run form. Args carries the resolved
// argument map with {task} substituted but {stepN.field} placeholders
// left intact (since step N hasn't run yet).
type PlanStep struct {
	Index  int
	Kind   string // "tool" | "chat"
	Name   string
	Args   map[string]any
	System string // chat-step system message preview
	Input  string // chat-step input preview
}

// Execute runs every step of the (recipe, tier) pair in order, threading
// outputs through the template context. Returns the first step error it
// encounters along with a partial Execution containing the steps that
// did complete.
func (e *Executor) Execute(ctx context.Context, rec Recipe, tier, task string) (*Execution, error) {
	tr, ok := rec.Recipes[tier]
	if !ok {
		return nil, fmt.Errorf("recipe %q: no %q tier", rec.ID, tier)
	}
	exec := &Execution{RecipeID: rec.ID, Tier: tier, Task: task}
	tc := &templateContext{task: task, stepOutputs: make([]map[string]any, 0, len(tr.Steps))}

	for i, step := range tr.Steps {
		idx := i + 1
		out, err := e.runStep(ctx, idx, step, tc, tier)
		exec.Steps = append(exec.Steps, out)
		exec.TotalCostUSD += out.CostUSD
		exec.TotalLatencyMS += out.LatencyMS
		if err != nil {
			return exec, fmt.Errorf("step %d (%s %s): %w", idx, out.Kind, out.Name, err)
		}
		if step.IsChat() {
			exec.FinalOutput = out.OutputText
		} else if exec.FinalOutput == "" {
			// A recipe that ends on a tool step (no chat synthesis) still
			// reports something useful as the final output.
			exec.FinalOutput = out.OutputText
		}
	}
	return exec, nil
}

func (e *Executor) runStep(ctx context.Context, idx int, step Step, tc *templateContext, tier string) (StepOutcome, error) {
	switch {
	case step.IsTool():
		return e.runTool(ctx, idx, step, tc)
	case step.IsChat():
		return e.runChat(ctx, idx, step, tc, tier)
	default:
		return StepOutcome{Index: idx}, fmt.Errorf("step %d: neither tool nor chat (malformed recipe)", idx)
	}
}

func (e *Executor) runTool(ctx context.Context, idx int, step Step, tc *templateContext) (StepOutcome, error) {
	resolved := resolveArgs(step.With, tc)
	res, err := e.Tools.CallTool(ctx, step.Tool, resolved)
	out := StepOutcome{Index: idx, Kind: "tool", Name: step.Tool}
	if err != nil {
		tc.stepOutputs = append(tc.stepOutputs, nil)
		return out, err
	}
	out.CostUSD = floatField(res.Structured, "cost_usd")
	out.LatencyMS = int64(floatField(res.Structured, "latency_ms"))
	if p, ok := res.Structured["provider_used"].(string); ok {
		out.Provider = p
	}
	if res.Content != "" {
		out.OutputText = res.Content
	} else {
		out.OutputText = formatStructured(res.Structured)
	}
	tc.stepOutputs = append(tc.stepOutputs, res.Structured)
	return out, nil
}

func (e *Executor) runChat(ctx context.Context, idx int, step Step, tc *templateContext, tier string) (StepOutcome, error) {
	cs := step.Chat
	req := ChatRequest{
		Model:   cs.Model,
		Quality: tier,
		System:  resolveString(cs.System, tc),
		Input:   resolveString(cs.Input, tc),
	}
	resp, err := e.Chat.Chat(ctx, req)
	out := StepOutcome{Index: idx, Kind: "chat", Name: cs.Model}
	if err != nil {
		tc.stepOutputs = append(tc.stepOutputs, nil)
		return out, err
	}
	out.Name = resp.Model // resolved (auto → concrete model name)
	out.OutputText = resp.Output
	out.CostUSD = resp.CostUSD
	out.LatencyMS = resp.LatencyMS
	tc.stepOutputs = append(tc.stepOutputs, map[string]any{"output": resp.Output})
	return out, nil
}

// Plan walks the recipe without making network calls. Used by `frugal
// route` to preview the routing decision.
func (e *Executor) Plan(rec Recipe, tier, task string) (*Plan, error) {
	tr, ok := rec.Recipes[tier]
	if !ok {
		return nil, fmt.Errorf("recipe %q: no %q tier", rec.ID, tier)
	}
	p := &Plan{RecipeID: rec.ID, Tier: tier, Task: task, Reason: tr.Reason}
	tc := &templateContext{task: task}
	for i, step := range tr.Steps {
		idx := i + 1
		ps := PlanStep{Index: idx}
		switch {
		case step.IsTool():
			ps.Kind = "tool"
			ps.Name = step.Tool
			ps.Args = resolveArgs(step.With, tc)
		case step.IsChat():
			ps.Kind = "chat"
			ps.System = resolveString(step.Chat.System, tc)
			ps.Input = resolveString(step.Chat.Input, tc)
			req := ChatRequest{Model: step.Chat.Model, Quality: tier, System: ps.System, Input: ps.Input}
			ps.Name = e.Chat.Resolve(req)
		}
		p.Steps = append(p.Steps, ps)
	}
	return p, nil
}

// templateContext holds {task} and prior step outputs for placeholder
// resolution. Caller is responsible for appending step outputs in order.
type templateContext struct {
	task        string
	stepOutputs []map[string]any
}

// templateRE matches {task}, {step1.results}, {step12.output}, etc. The
// key syntax is intentionally restrictive — letters, digits, underscores,
// and one dot — so the rendering rule is predictable and the regex can't
// be tricked by adjacent braces in user content.
var templateRE = regexp.MustCompile(`\{([a-zA-Z][a-zA-Z0-9_]*(?:\.[a-zA-Z][a-zA-Z0-9_]*)?)\}`)

// resolveString replaces every {key} in s with its rendered value from
// tc. Unknown keys are left literal so a malformed recipe surfaces in the
// LLM call rather than silently dropping fields.
func resolveString(s string, tc *templateContext) string {
	if s == "" || tc == nil {
		return s
	}
	return templateRE.ReplaceAllStringFunc(s, func(match string) string {
		key := match[1 : len(match)-1]
		v, ok := tc.lookup(key)
		if !ok {
			return match
		}
		return v
	})
}

// resolveArgs walks a map of step args, resolving template strings in
// string-valued entries. Non-string values pass through unchanged.
func resolveArgs(args map[string]any, tc *templateContext) map[string]any {
	if args == nil {
		return nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		if s, ok := v.(string); ok {
			out[k] = resolveString(s, tc)
		} else {
			out[k] = v
		}
	}
	return out
}

func (tc *templateContext) lookup(key string) (string, bool) {
	if key == "task" {
		return tc.task, true
	}
	if !strings.HasPrefix(key, "step") {
		return "", false
	}
	rest := strings.TrimPrefix(key, "step")
	dot := strings.IndexByte(rest, '.')
	if dot < 0 {
		return "", false
	}
	n, err := strconv.Atoi(rest[:dot])
	if err != nil || n < 1 || n > len(tc.stepOutputs) {
		return "", false
	}
	field := rest[dot+1:]
	out := tc.stepOutputs[n-1]
	if out == nil {
		return "", false
	}
	v, ok := out[field]
	if !ok {
		return "", false
	}
	return formatValue(v), true
}

// formatValue renders one step-output field as a string the next step
// can consume. Result lists (array of {title, url, snippet} objects) get
// formatted as a numbered list for chat models; everything else falls
// back to JSON.
func formatValue(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if arr, ok := v.([]any); ok {
		// Recognize a "result list" shape: items are objects with title +
		// url + snippet. Render as a numbered list — LLMs handle this
		// better than raw JSON.
		var b strings.Builder
		isResults := true
		for i, item := range arr {
			m, ok := item.(map[string]any)
			if !ok {
				isResults = false
				break
			}
			if _, hasTitle := m["title"]; !hasTitle {
				isResults = false
				break
			}
			fmt.Fprintf(&b, "[%d] %s\n", i+1, strOf(m["title"]))
			if url := strOf(m["url"]); url != "" {
				fmt.Fprintf(&b, "    %s\n", url)
			}
			if snip := strOf(m["snippet"]); snip != "" {
				fmt.Fprintf(&b, "    %s\n", snip)
			}
		}
		if isResults {
			return strings.TrimRight(b.String(), "\n")
		}
	}
	buf, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(buf)
}

func strOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func floatField(m map[string]any, k string) float64 {
	if v, ok := m[k]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case float32:
			return float64(n)
		case int:
			return float64(n)
		case int64:
			return float64(n)
		case json.Number:
			f, _ := n.Float64()
			return f
		}
	}
	return 0
}

// formatStructured renders the whole structured payload as JSON. Used as
// the OutputText when the tool didn't supply a plain-text Content field.
func formatStructured(m map[string]any) string {
	if m == nil {
		return ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// SinceMS returns elapsed milliseconds since t — used by executor impls
// to set StepOutcome.LatencyMS in a consistent way.
func SinceMS(t time.Time) int64 { return time.Since(t).Milliseconds() }
