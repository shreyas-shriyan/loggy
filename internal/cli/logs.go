package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/shreyas-shriyan/loggy/internal/awsauth"
	"github.com/shreyas-shriyan/loggy/internal/config"
	"github.com/shreyas-shriyan/loggy/internal/cwlogs"
	"github.com/shreyas-shriyan/loggy/internal/ecs"
	"github.com/shreyas-shriyan/loggy/internal/eks"
	"github.com/shreyas-shriyan/loggy/internal/stream"
	"github.com/shreyas-shriyan/loggy/internal/tui"
	"github.com/shreyas-shriyan/loggy/internal/ui"
	corev1 "k8s.io/api/core/v1"
)

// dashboard runs the full-screen UI for env, leaving it whenever a prompt is needed.
// extra holds unsaved targets (from `loggy menu`) shown after the saved ones.
func (a *App) dashboard(env string, extra []config.Target) error {
	ctx := context.Background()
	cursor := 0
	if len(extra) > 0 {
		cursor = len(a.cfg.TargetsFor(env))
	}
	var auto *ui.Request
	flash := ""

	for {
		targets := a.cfg.TargetsFor(env)
		items := make([]ui.Item, 0, len(targets)+len(extra))
		for _, t := range targets {
			items = append(items, ui.Item{Target: t, Region: a.cfg.RegionFor(t)})
		}
		for _, t := range extra {
			items = append(items, ui.Item{Target: t, Region: a.cfg.RegionFor(t), Unsaved: true})
		}

		act, err := ui.Run(ui.Options{
			Env:       env,
			Profile:   a.cfg.Env(env).Profile,
			Items:     items,
			Cursor:    cursor,
			Start:     a.startStream,
			AutoStart: auto,
			Flash:     flash,
		})
		if err != nil {
			return err
		}
		cursor, auto, flash = act.Cursor, nil, ""

		switch act.Kind {
		case ui.ActQuit:
			return nil
		case ui.ActSwitchEnv:
			env, cursor, extra = otherEnv(env), 0, nil
		case ui.ActAdd:
			err = a.addTarget(ctx, env)
			if err == nil {
				flash = "Server list updated."
			}
		case ui.ActRemove:
			err = a.removeTargets(env)
		case ui.ActSettings:
			err = a.configureEnv(env)
		case ui.ActSaveTarget:
			if err = a.saveTarget(act.Target); err == nil {
				extra = removeTarget(extra, act.Target)
				flash = "Saved " + act.Target.Primary() + "."
			}
		case ui.ActCustom:
			var req ui.Request
			if req, err = a.customRequest(ctx, act.Target); err == nil {
				auto = &req
			}
		case ui.ActSave:
			flash, err = a.saveFlow(ctx, act.Target)
		case ui.ActLogin:
			if _, err = a.targetSession(ctx, act.Target); err == nil {
				req := act.Request
				auto = &req
			}
		}
		if err != nil && !errors.Is(err, tui.ErrBack) {
			flash = "Error: " + err.Error()
		}
	}
}

func removeTarget(list []config.Target, t config.Target) []config.Target {
	out := list[:0]
	for _, x := range list {
		if x.Key() != t.Key() {
			out = append(out, x)
		}
	}
	return out
}

// startStream is the dashboard's StartFunc; it must not prompt or print.
func (a *App) startStream(ctx context.Context, t config.Target, req ui.Request) (ui.Runner, error) {
	s, err := a.quietSession(ctx, a.cfg.ProfileFor(t), a.cfg.RegionFor(t))
	if err != nil {
		return nil, err
	}
	run, err := a.buildRunner(ctx, s, t, req.Since, req.Pod, true)
	if err != nil {
		return nil, err
	}
	filter, err := stream.NewFilter(false, req.Pattern)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, sink func(string)) error {
		return run(ctx, func(l string) {
			if filter.Keep(l) {
				sink(l)
			}
		})
	}, nil
}

// buildRunner connects to the target and returns a function that streams its logs.
func (a *App) buildRunner(ctx context.Context, s *awsauth.Session, t config.Target, since time.Duration, pod string, follow bool) (ui.Runner, error) {
	switch t.Type {
	case config.TypeEKS:
		cs, err := a.kubeClient(ctx, s, t.Cluster)
		if err != nil {
			return nil, err
		}
		pods, err := a.targetPods(ctx, s, t)
		if err != nil {
			return nil, err
		}
		if pod != "" {
			pods = filterPods(pods, pod)
			if len(pods) == 0 {
				return nil, fmt.Errorf("pod %s is no longer running", pod)
			}
		}
		opts := eks.LogOptions{Follow: follow, Since: since}
		return func(ctx context.Context, sink func(string)) error {
			return eks.StreamLogs(ctx, cs, t.Namespace, pods, opts, sink)
		}, nil

	case config.TypeECS:
		group := t.LogGroup
		if group == "" {
			groups, err := ecs.LogGroups(ctx, s.Config, t.Cluster, t.Service)
			if err != nil {
				return nil, err
			}
			group = groups[0]
		}
		api := cwlogs.New(s.Config)
		start := time.Now().Add(-since)
		return func(ctx context.Context, sink func(string)) error {
			return cwlogs.Tail(ctx, api, group, start, follow, sink)
		}, nil

	}
	return nil, fmt.Errorf("%s: unsupported target type %q", t.Primary(), t.Type)
}

func (a *App) targetPods(ctx context.Context, s *awsauth.Session, t config.Target) ([]corev1.Pod, error) {
	cs, err := a.kubeClient(ctx, s, t.Cluster)
	if err != nil {
		return nil, err
	}
	selector, err := eks.ServiceSelector(ctx, cs, t.Namespace, t.Service)
	if err != nil {
		return nil, err
	}
	all, err := eks.ListPods(ctx, cs, t.Namespace, selector)
	if err != nil {
		return nil, err
	}
	var pods []corev1.Pod
	for _, p := range all {
		if p.Status.Phase != corev1.PodPending {
			pods = append(pods, p)
		}
	}
	if len(pods) == 0 {
		return nil, fmt.Errorf("no running pods for %s/%s (selector %s)", t.Namespace, t.Service, selector)
	}
	return pods, nil
}

func filterPods(pods []corev1.Pod, name string) []corev1.Pod {
	for _, p := range pods {
		if p.Name == name {
			return []corev1.Pod{p}
		}
	}
	return nil
}

// customRequest prompts for time range, filter and (for EKS) a single pod.
func (a *App) customRequest(ctx context.Context, t config.Target) (ui.Request, error) {
	since, err := chooseSince()
	if err != nil {
		return ui.Request{}, err
	}
	pattern, err := promptPattern()
	if err != nil {
		return ui.Request{}, err
	}
	showHealth, err := tui.Confirm("Show health-check lines?", false)
	if err != nil {
		return ui.Request{}, err
	}
	req := ui.Request{Since: since, Pattern: pattern, ShowHealth: showHealth}

	if t.Type == config.TypeEKS {
		s, err := a.targetSession(ctx, t)
		if err != nil {
			return ui.Request{}, err
		}
		pods, err := a.targetPods(ctx, s, t)
		if err != nil {
			return ui.Request{}, err
		}
		if len(pods) > 1 {
			opts := []tui.Opt{{Key: "", Label: fmt.Sprintf("All pods (%d)", len(pods))}}
			for _, p := range pods {
				opts = append(opts, tui.Opt{Key: p.Name, Label: p.Name + tui.Faint("  "+string(p.Status.Phase))})
			}
			if req.Pod, err = tui.Select("Pods", opts); err != nil {
				return ui.Request{}, err
			}
		}
	}
	return req, nil
}

// saveFlow prompts for a range and filter, then writes logs to a file.
func (a *App) saveFlow(ctx context.Context, t config.Target) (string, error) {
	since, err := chooseSince()
	if err != nil {
		return "", err
	}
	hideHealth, err := tui.Confirm("Hide health-check noise?", true)
	if err != nil {
		return "", err
	}
	pattern, err := promptPattern()
	if err != nil {
		return "", err
	}
	filter, err := stream.NewFilter(hideHealth, pattern)
	if err != nil {
		return "", err
	}
	s, err := a.targetSession(ctx, t)
	if err != nil {
		return "", err
	}
	run, err := a.buildRunner(ctx, s, t, since, "", false)
	if err != nil {
		return "", err
	}

	f, err := stream.CreateLogFile(config.LogDir(), t.Env+"-"+t.Primary(), time.Now())
	if err != nil {
		return "", err
	}
	a.printf("Saving %s logs (last %s) to %s ... Press Ctrl+C to stop early.\n", t.Primary(), since, f.Name())

	sctx, stop := stream.Interruptible(context.Background())
	defer stop()
	sink := stream.NewSink(f, filter)
	runErr := run(sctx, sink.Write)
	closeErr := f.Close()
	interrupted := sctx.Err() != nil
	if runErr != nil && !interrupted {
		return "", runErr
	}
	if err := errors.Join(sink.Err(), closeErr); err != nil {
		return "", err
	}

	n := sink.Lines()
	if n == 0 {
		_ = os.Remove(f.Name())
		return "No log lines matched; nothing saved.", nil
	}
	note := ""
	if interrupted {
		note = " (stopped early)"
	}
	return fmt.Sprintf("Saved %d line(s) to %s%s", n, f.Name(), note), nil
}

func promptPattern() (string, error) {
	return tui.Input("Keyword or regex filter (optional)", "", "e.g. (?i)error|timeout", func(s string) error {
		_, err := stream.NewFilter(false, s)
		return err
	})
}

func chooseSince() (time.Duration, error) {
	var opts []tui.Opt
	for _, v := range []string{"5m", "15m", "30m", "1h", "2h", "6h", "1d"} {
		opts = append(opts, tui.Opt{Key: v, Label: "Last " + v})
	}
	opts = append(opts, tui.Opt{Key: keyCustom, Label: "Custom range..."})
	v, err := tui.Select("Time range", opts)
	if err != nil {
		return 0, err
	}
	if v == keyCustom {
		v, err = tui.Input("How far back? (e.g. 45m, 3h, 2d)", "", "45m", func(s string) error {
			_, err := stream.ParseSince(s)
			return err
		})
		if err != nil {
			return 0, err
		}
	}
	return stream.ParseSince(v)
}
