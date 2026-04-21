package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/frugalsh/frugal/internal/classifier"
	"github.com/frugalsh/frugal/internal/config"
	"github.com/frugalsh/frugal/internal/eval"
	"github.com/frugalsh/frugal/internal/provider"
	"github.com/frugalsh/frugal/internal/provider/anthropic"
	"github.com/frugalsh/frugal/internal/provider/google"
	"github.com/frugalsh/frugal/internal/provider/openai"
	"github.com/frugalsh/frugal/internal/router"
	"github.com/frugalsh/frugal/internal/types"
)

const defaultBenchWorkload = "config/workloads/starter.yaml"

// runBench executes `frugal bench [-workload PATH] [-quality TIER] [-out FILE]
// [-timeout DUR]`. Returns the process exit code.
//
// Example: `frugal bench -quality balanced -out bench.md`.
func runBench(configPath string, args []string) int {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	workloadPath := fs.String("workload", defaultBenchWorkload, "path to workload YAML")
	qualityStr := fs.String("quality", "balanced", "quality tier (high | balanced | cost)")
	outPath := fs.String("out", "", "write markdown report to this path in addition to stdout")
	timeout := fs.Duration("timeout", 10*time.Minute, "overall bench timeout")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: frugal bench [flags]")
		fmt.Fprintln(os.Stderr, "Measures cost and quality for every problem in the workload, calling")
		fmt.Fprintln(os.Stderr, "real provider APIs. Requires whichever provider keys the workload's")
		fmt.Fprintln(os.Stderr, "models + baseline need (OPENAI_API_KEY, ANTHROPIC_API_KEY, GOOGLE_API_KEY).")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	quality, ok := types.ParseQualityThreshold(*qualityStr)
	if !ok {
		fmt.Fprintf(os.Stderr, "frugal bench: unknown quality %q (want high | balanced | cost)\n", *qualityStr)
		return 2
	}

	resolvedWorkload := *workloadPath
	if !filepath.IsAbs(resolvedWorkload) {
		if _, err := os.Stat(resolvedWorkload); err != nil {
			// Fall back to the installed config dir when running from elsewhere.
			if p := os.Getenv("FRUGAL_CONFIG_DIR"); p != "" {
				alt := filepath.Join(p, filepath.Base(resolvedWorkload))
				if _, err := os.Stat(alt); err == nil {
					resolvedWorkload = alt
				}
			}
		}
	}

	w, err := eval.LoadLiveWorkload(resolvedWorkload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "frugal bench: %v\n", err)
		return 1
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "frugal bench: load config: %v\n", err)
		return 1
	}

	reg := provider.NewRegistry()
	registerBenchProviders(cfg, reg)
	if len(reg.AllModels()) == 0 {
		fmt.Fprintln(os.Stderr, "frugal bench: no API keys found. Set OPENAI_API_KEY, ANTHROPIC_API_KEY, or GOOGLE_API_KEY.")
		return 1
	}

	if _, err := reg.Resolve(w.Baseline); err != nil {
		fmt.Fprintf(os.Stderr, "frugal bench: baseline model %q not registered (no key for its provider?): %v\n",
			w.Baseline, err)
		return 1
	}

	modelEntries, thresholds := router.BuildTaxonomy(cfg)
	modelEntries = filterRegisteredModels(modelEntries, reg)
	if len(modelEntries) == 0 {
		fmt.Fprintln(os.Stderr, "frugal bench: no routable models available for registered providers")
		return 1
	}
	rtr := router.New(modelEntries, thresholds)
	cls := classifier.NewRuleBased()
	runner := eval.NewLiveRunner(cfg, cls, rtr, reg)

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	fmt.Fprintf(os.Stderr, "running %d problems from %q against baseline %s @ quality=%s...\n",
		len(w.Problems), w.Name, w.Baseline, quality)
	summary, err := runner.Run(ctx, w, quality)
	if err != nil {
		fmt.Fprintf(os.Stderr, "frugal bench: %v\n", err)
		return 1
	}

	// Always write to stdout.
	var buf bytes.Buffer
	if err := eval.WriteLiveMarkdown(&buf, summary); err != nil {
		fmt.Fprintf(os.Stderr, "frugal bench: render report: %v\n", err)
		return 1
	}
	fmt.Print(buf.String())

	if *outPath != "" {
		if err := os.WriteFile(*outPath, buf.Bytes(), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "frugal bench: write %s: %v\n", *outPath, err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "wrote %s\n", *outPath)
	}

	// Print a one-line summary on stderr for CI log scraping.
	fmt.Fprintf(os.Stderr,
		"bench done: %d problems · frugal %.1f%% pass · baseline %.1f%% pass · savings %.1f%%\n",
		summary.ProblemCount, summary.FrugalPassRate, summary.BaselinePassRate, summary.SavingsPct)
	return 0
}

// registerBenchProviders mirrors runWrap's provider registration without the
// retry wrapper: benchmarks should see raw upstream behavior so retries don't
// mask quality/rate-limit issues.
func registerBenchProviders(cfg *config.Config, reg *provider.Registry) {
	if pc, ok := cfg.Providers["openai"]; ok {
		if key := os.Getenv(pc.APIKeyEnv); key != "" {
			reg.Register(openai.New(key, pc.BaseURL, modelNames(pc)))
		}
	}
	if pc, ok := cfg.Providers["anthropic"]; ok {
		if key := os.Getenv(pc.APIKeyEnv); key != "" {
			reg.Register(anthropic.New(key, pc.BaseURL, modelNames(pc)))
		}
	}
	if pc, ok := cfg.Providers["google"]; ok {
		if key := os.Getenv(pc.APIKeyEnv); key != "" {
			reg.Register(google.New(key, pc.BaseURL, modelNames(pc)))
		}
	}
}
