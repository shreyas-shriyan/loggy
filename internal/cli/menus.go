package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/shreyas-shriyan/loggy/internal/awsauth"
	"github.com/shreyas-shriyan/loggy/internal/config"
	"github.com/shreyas-shriyan/loggy/internal/ecs"
	"github.com/shreyas-shriyan/loggy/internal/eks"
	"github.com/shreyas-shriyan/loggy/internal/tui"
)

const keyCustom = "__custom__"

// setup adds targets until the user stops.
func (a *App) setup(env string) error {
	ctx := context.Background()
	for {
		if err := a.addTarget(ctx, env); err != nil {
			return err
		}
		more, err := tui.Confirm("Add another server?", false)
		if err != nil || !more {
			return err
		}
	}
}

// adhocMenu browses a cluster and service, then opens it in the dashboard unsaved.
func (a *App) adhocMenu(env string) error {
	if err := a.ensureEnvConfigured(env); err != nil {
		return err
	}
	t, err := a.pickTarget(context.Background(), env)
	if err != nil {
		return err
	}
	return a.dashboard(env, []config.Target{t})
}

func (a *App) addTarget(ctx context.Context, env string) error {
	if err := a.ensureEnvConfigured(env); err != nil {
		return err
	}
	t, err := a.pickTarget(ctx, env)
	if err != nil {
		return err
	}
	return a.saveTarget(t)
}

func (a *App) saveTarget(t config.Target) error {
	if !a.cfg.AddTarget(t) {
		a.printf("%s is already saved.\n", t.Primary())
		return nil
	}
	if err := a.cfg.Save(); err != nil {
		return err
	}
	a.printf("Saved %s to %s\n", t.Label(a.cfg.RegionFor(t)), a.cfg.Path())
	return nil
}

func (a *App) ensureEnvConfigured(env string) error {
	if a.cfg.Env(env).Region != "" {
		return nil
	}
	a.printf("The %s environment has no AWS region yet; configure it first.\n", env)
	return a.configureEnv(env)
}

// pickTarget walks the user through choosing an EKS or ECS service.
func (a *App) pickTarget(ctx context.Context, env string) (config.Target, error) {
	typ, err := tui.Select("Target type", []tui.Opt{
		{Key: config.TypeEKS, Label: "EKS (Kubernetes pod logs)"},
		{Key: config.TypeECS, Label: "ECS (CloudWatch Logs)"},
	})
	if err != nil {
		return config.Target{}, err
	}
	s, err := a.envSession(ctx, env)
	if err != nil {
		return config.Target{}, err
	}
	if typ == config.TypeEKS {
		return a.pickEKS(ctx, env, s)
	}
	return a.pickECS(ctx, env, s)
}

func (a *App) pickEKS(ctx context.Context, env string, s *awsauth.Session) (config.Target, error) {
	clusters, err := eks.ListClusters(ctx, s.Config)
	if err != nil {
		return config.Target{}, err
	}
	if def := a.cfg.Env(env).EKSCluster; def != "" {
		clusters = preferFirst(clusters, def)
	}
	if len(clusters) == 0 {
		return config.Target{}, fmt.Errorf("no EKS clusters found in %s", s.Config.Region)
	}
	cluster, err := tui.SelectStrings("EKS cluster", clusters, nil)
	if err != nil {
		return config.Target{}, err
	}
	cs, err := a.kubeClient(ctx, s, cluster)
	if err != nil {
		return config.Target{}, err
	}
	namespaces, err := eks.ListNamespaces(ctx, cs)
	if err != nil {
		return config.Target{}, err
	}
	ns, err := tui.SelectStrings("Namespace", namespaces, nil)
	if err != nil {
		return config.Target{}, err
	}
	services, err := eks.ListServices(ctx, cs, ns)
	if err != nil {
		return config.Target{}, err
	}
	if len(services) == 0 {
		return config.Target{}, fmt.Errorf("no services with pod selectors in namespace %s", ns)
	}
	svc, err := tui.SelectStrings("Service", services, nil)
	if err != nil {
		return config.Target{}, err
	}
	return config.Target{Name: svc, Env: env, Type: config.TypeEKS, Cluster: cluster, Namespace: ns, Service: svc}, nil
}

func (a *App) pickECS(ctx context.Context, env string, s *awsauth.Session) (config.Target, error) {
	clusters, err := ecs.ListClusters(ctx, s.Config)
	if err != nil {
		return config.Target{}, err
	}
	if len(clusters) == 0 {
		return config.Target{}, fmt.Errorf("no ECS clusters found in %s", s.Config.Region)
	}
	cluster, err := tui.SelectStrings("ECS cluster", clusters, config.ShortName)
	if err != nil {
		return config.Target{}, err
	}
	services, err := ecs.ListServices(ctx, s.Config, cluster)
	if err != nil {
		return config.Target{}, err
	}
	if len(services) == 0 {
		return config.Target{}, fmt.Errorf("no services in ECS cluster %s", config.ShortName(cluster))
	}
	svc, err := tui.SelectStrings("ECS service", services, config.ShortName)
	if err != nil {
		return config.Target{}, err
	}
	group, err := a.chooseLogGroup(ctx, s, cluster, svc)
	if err != nil {
		return config.Target{}, err
	}
	return config.Target{Name: config.ShortName(svc), Env: env, Type: config.TypeECS, Cluster: cluster, Service: svc, LogGroup: group}, nil
}

func (a *App) chooseLogGroup(ctx context.Context, s *awsauth.Session, cluster, service string) (string, error) {
	groups, err := ecs.LogGroups(ctx, s.Config, cluster, service)
	if err != nil {
		return "", err
	}
	if len(groups) == 1 {
		return groups[0], nil
	}
	return tui.SelectStrings("Log group", groups, nil)
}

func (a *App) removeTargets(env string) error {
	targets := a.cfg.TargetsFor(env)
	if len(targets) == 0 {
		a.printf("No saved servers for %s.\n", env)
		return nil
	}
	opts := make([]tui.Opt, len(targets))
	for i, t := range targets {
		opts[i] = tui.Opt{Key: t.Key(), Label: tui.TargetLabel(t, a.cfg.RegionFor(t))}
	}
	keys, err := tui.MultiSelect("Remove which servers? (space to select, enter to confirm)", opts)
	if err != nil || len(keys) == 0 {
		return err
	}
	ok, err := tui.Confirm(fmt.Sprintf("Remove %d server(s)?", len(keys)), false)
	if err != nil || !ok {
		return err
	}
	n := a.cfg.RemoveTargets(keys)
	if err := a.cfg.Save(); err != nil {
		return err
	}
	a.printf("Removed %d server(s).\n", n)
	return nil
}

func (a *App) configureEnv(env string) error {
	e := a.cfg.Env(env)

	profiles := awsauth.ListProfiles()
	opts := make([]tui.Opt, 0, len(profiles)+1)
	for _, p := range profiles {
		label := p
		if p == e.Profile {
			label += tui.Faint("  (current)")
		}
		opts = append(opts, tui.Opt{Key: p, Label: label})
	}
	opts = append(opts, tui.Opt{Key: keyCustom, Label: "Type a profile name..."})
	profile, err := tui.Select(fmt.Sprintf("AWS profile for %s", env), opts)
	if err != nil {
		return err
	}
	if profile == keyCustom {
		profile, err = tui.Input("AWS profile name", e.Profile, "my-sso-profile", nonEmpty)
		if err != nil {
			return err
		}
	}

	region := e.Region
	if region == "" {
		region = "us-east-1"
	}
	region, err = tui.Input(fmt.Sprintf("Default AWS region for %s", env), region, "us-east-1", nonEmpty)
	if err != nil {
		return err
	}

	e.Profile, e.Region = profile, region
	a.cfg.SetEnv(env, e)
	if err := a.cfg.Save(); err != nil {
		return err
	}
	a.forgetSessions()
	a.printf("Saved %s settings: profile %s, region %s\n", env, profile, region)
	return nil
}

func nonEmpty(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("required")
	}
	return nil
}

// preferFirst moves v to the front of list if present.
func preferFirst(list []string, v string) []string {
	out := make([]string, 0, len(list))
	found := false
	for _, s := range list {
		if s == v {
			found = true
			continue
		}
		out = append(out, s)
	}
	if !found {
		return list
	}
	return append([]string{v}, out...)
}
