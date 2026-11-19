// Package eks connects to EKS clusters without a kubeconfig and streams pod logs.
package eks

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awseks "github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// MaxLogStreams caps concurrent container log streams, like kubectl --max-log-requests.
const MaxLogStreams = 20

const (
	tokenPrefix   = "k8s-aws-v1."
	clusterHeader = "x-k8s-aws-id"
	tokenLifetime = 10 * time.Minute
)

// ListClusters returns the EKS cluster names in the configured region.
func ListClusters(ctx context.Context, cfg aws.Config) ([]string, error) {
	var names []string
	p := awseks.NewListClustersPaginator(awseks.NewFromConfig(cfg), &awseks.ListClustersInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		names = append(names, page.Clusters...)
	}
	sort.Strings(names)
	return names, nil
}

// Connect builds a Kubernetes client for cluster authenticated with an STS-presigned token.
func Connect(ctx context.Context, cfg aws.Config, cluster string) (kubernetes.Interface, error) {
	out, err := awseks.NewFromConfig(cfg).DescribeCluster(ctx, &awseks.DescribeClusterInput{Name: aws.String(cluster)})
	if err != nil {
		return nil, fmt.Errorf("describe EKS cluster %s: %w", cluster, err)
	}
	c := out.Cluster
	if c == nil || c.Endpoint == nil || c.CertificateAuthority == nil || c.CertificateAuthority.Data == nil {
		return nil, fmt.Errorf("EKS cluster %s has no endpoint or CA yet", cluster)
	}
	ca, err := base64.StdEncoding.DecodeString(aws.ToString(c.CertificateAuthority.Data))
	if err != nil {
		return nil, fmt.Errorf("decode cluster CA: %w", err)
	}

	tokens := &tokenSource{sts: sts.NewPresignClient(sts.NewFromConfig(cfg)), cluster: cluster}
	if _, err := tokens.Token(ctx); err != nil {
		return nil, err
	}
	rc := &rest.Config{
		Host:            aws.ToString(c.Endpoint),
		TLSClientConfig: rest.TLSClientConfig{CAData: ca},
		WrapTransport: func(rt http.RoundTripper) http.RoundTripper {
			return &bearerTransport{base: rt, tokens: tokens}
		},
	}
	return kubernetes.NewForConfig(rc)
}

type tokenSource struct {
	sts     *sts.PresignClient
	cluster string

	mu      sync.Mutex
	token   string
	expires time.Time
}

func (t *tokenSource) Token(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.token != "" && time.Now().Before(t.expires) {
		return t.token, nil
	}
	req, err := t.sts.PresignGetCallerIdentity(ctx, &sts.GetCallerIdentityInput{}, func(o *sts.PresignOptions) {
		o.ClientOptions = append(o.ClientOptions, func(so *sts.Options) {
			so.APIOptions = append(so.APIOptions,
				smithyhttp.SetHeaderValue(clusterHeader, t.cluster),
				smithyhttp.SetHeaderValue("X-Amz-Expires", "60"),
			)
		})
	})
	if err != nil {
		return "", fmt.Errorf("create EKS token: %w", err)
	}
	t.token = tokenPrefix + base64.RawURLEncoding.EncodeToString([]byte(req.URL))
	t.expires = time.Now().Add(tokenLifetime)
	return t.token, nil
}

type bearerTransport struct {
	base   http.RoundTripper
	tokens *tokenSource
}

func (b *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tok, err := b.tokens.Token(req.Context())
	if err != nil {
		return nil, err
	}
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+tok)
	return b.base.RoundTrip(r)
}

// ListNamespaces returns namespace names in the cluster.
func ListNamespaces(ctx context.Context, cs kubernetes.Interface) ([]string, error) {
	list, err := cs.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(list.Items))
	for _, n := range list.Items {
		names = append(names, n.Name)
	}
	sort.Strings(names)
	return names, nil
}

// ListServices returns service names in namespace that have a pod selector.
func ListServices(ctx context.Context, cs kubernetes.Interface, namespace string) ([]string, error) {
	list, err := cs.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var names []string
	for _, s := range list.Items {
		if len(s.Spec.Selector) > 0 {
			names = append(names, s.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// ServiceSelector returns the label selector of a Kubernetes service.
func ServiceSelector(ctx context.Context, cs kubernetes.Interface, namespace, service string) (string, error) {
	svc, err := cs.CoreV1().Services(namespace).Get(ctx, service, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("read service %s/%s: %w", namespace, service, err)
	}
	if len(svc.Spec.Selector) == 0 {
		return "", fmt.Errorf("service %s/%s has no pod selector", namespace, service)
	}
	return labels.SelectorFromSet(svc.Spec.Selector).String(), nil
}

// ListPods returns the pods matching selector, sorted by name.
func ListPods(ctx context.Context, cs kubernetes.Interface, namespace, selector string) ([]corev1.Pod, error) {
	list, err := cs.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}
	pods := list.Items
	sort.Slice(pods, func(i, j int) bool { return pods[i].Name < pods[j].Name })
	return pods, nil
}

// LogOptions controls a log fetch.
type LogOptions struct {
	Follow bool
	// Since limits logs to this window; zero means no limit.
	Since time.Duration
	// TailLines limits the initial lines per container; zero means no limit.
	TailLines int64
}

// StreamLogs fetches logs from every container of pods and passes each line,
// prefixed with [pod/container], to sink. sink must be safe for concurrent use.
// It returns when all streams end or ctx is cancelled.
func StreamLogs(ctx context.Context, cs kubernetes.Interface, namespace string, pods []corev1.Pod, opts LogOptions, sink func(string)) error {
	type stream struct{ pod, container string }
	var streams []stream
	for _, p := range pods {
		for _, c := range p.Spec.Containers {
			streams = append(streams, stream{p.Name, c.Name})
		}
	}
	if len(streams) == 0 {
		return errors.New("no containers to read logs from")
	}
	if len(streams) > MaxLogStreams {
		return fmt.Errorf("%d container streams exceed the limit of %d; pick a single pod", len(streams), MaxLogStreams)
	}

	var wg sync.WaitGroup
	errs := make([]error, len(streams))
	for i, s := range streams {
		wg.Add(1)
		go func(i int, s stream) {
			defer wg.Done()
			errs[i] = streamContainer(ctx, cs, namespace, s.pod, s.container, opts, sink)
		}(i, s)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.Join(errs...)
}

func streamContainer(ctx context.Context, cs kubernetes.Interface, namespace, pod, container string, opts LogOptions, sink func(string)) error {
	plo := &corev1.PodLogOptions{Container: container, Follow: opts.Follow, Timestamps: true}
	if opts.Since > 0 {
		secs := int64(opts.Since.Seconds())
		plo.SinceSeconds = &secs
	}
	if opts.TailLines > 0 {
		plo.TailLines = &opts.TailLines
	}
	rc, err := cs.CoreV1().Pods(namespace).GetLogs(pod, plo).Stream(ctx)
	if err != nil {
		return fmt.Errorf("%s/%s: %w", pod, container, err)
	}
	defer rc.Close()

	prefix := fmt.Sprintf("[%s/%s] ", pod, container)
	sc := bufio.NewScanner(rc)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		sink(prefix + sc.Text())
	}
	if err := sc.Err(); err != nil && ctx.Err() == nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s/%s: %w", pod, container, err)
	}
	return nil
}
