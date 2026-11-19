package cli

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/shreyas-shriyan/loggy/internal/awsauth"
	"github.com/shreyas-shriyan/loggy/internal/config"
	"github.com/shreyas-shriyan/loggy/internal/eks"
	"github.com/shreyas-shriyan/loggy/internal/tui"
	"github.com/shreyas-shriyan/loggy/internal/ui"
	"k8s.io/client-go/kubernetes"
)

// App holds state shared across menus in one loggy run.
type App struct {
	cfg *config.Config
	out io.Writer

	// mu guards sessions and kube, which the dashboard reads from background goroutines.
	mu       sync.Mutex
	sessions map[string]*awsauth.Session
	kube     map[string]kubernetes.Interface
}

func (a *App) init(cfg *config.Config) {
	a.cfg = cfg
	a.sessions = map[string]*awsauth.Session{}
	a.kube = map[string]kubernetes.Interface{}
}

func (a *App) printf(format string, args ...any) { fmt.Fprintf(a.out, format, args...) }

func (a *App) cachedSession(key string) (*awsauth.Session, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[key]
	return s, ok
}

func (a *App) storeSession(key string, s *awsauth.Session) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessions[key] = s
}

// quietSession returns verified credentials without prompting or printing; it
// wraps ui.ErrNeedLogin when the user has to sign in.
func (a *App) quietSession(ctx context.Context, profile, region string) (*awsauth.Session, error) {
	key := profile + "|" + region
	if s, ok := a.cachedSession(key); ok {
		return s, nil
	}
	cfg, err := awsauth.Load(ctx, profile, region)
	if err != nil {
		return nil, err
	}
	id, err := awsauth.Check(ctx, cfg)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %v", ui.ErrNeedLogin, err)
	}
	s := &awsauth.Session{Config: cfg, Identity: id, Profile: profile}
	a.storeSession(key, s)
	return s, nil
}

// session returns verified AWS credentials for profile and region, signing in when needed.
func (a *App) session(ctx context.Context, profile, region string) (*awsauth.Session, error) {
	key := profile + "|" + region
	if s, ok := a.cachedSession(key); ok {
		return s, nil
	}
	a.printf("Checking AWS credentials for profile %q (%s)...\n", displayProfile(profile), region)
	s, err := awsauth.Ensure(ctx, profile, region, a.out, func(prompt string) bool {
		ok, err := tui.Confirm(prompt, true)
		return err == nil && ok
	})
	if err != nil {
		return nil, err
	}
	a.printf("Signed in as %s\n", s.Identity.ARN)
	a.storeSession(key, s)
	return s, nil
}

func (a *App) targetSession(ctx context.Context, t config.Target) (*awsauth.Session, error) {
	return a.session(ctx, a.cfg.ProfileFor(t), a.cfg.RegionFor(t))
}

func (a *App) envSession(ctx context.Context, env string) (*awsauth.Session, error) {
	e := a.cfg.Env(env)
	return a.session(ctx, e.Profile, e.Region)
}

// kubeClient returns a cached Kubernetes client for cluster.
func (a *App) kubeClient(ctx context.Context, s *awsauth.Session, cluster string) (kubernetes.Interface, error) {
	key := s.Profile + "|" + s.Config.Region + "|" + cluster
	a.mu.Lock()
	cs, ok := a.kube[key]
	a.mu.Unlock()
	if ok {
		return cs, nil
	}
	cs, err := eks.Connect(ctx, s.Config, cluster)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.kube[key] = cs
	a.mu.Unlock()
	return cs, nil
}

func (a *App) forgetSessions() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessions = map[string]*awsauth.Session{}
	a.kube = map[string]kubernetes.Interface{}
}

func displayProfile(p string) string {
	if p == "" {
		return "default"
	}
	return p
}

func otherEnv(env string) string {
	if env == "dev" {
		return "prod"
	}
	return "dev"
}
