package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	e2ekeb "github.com/kyma-project/cloud-manager/e2e/keb"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/yaml"
)

type cmdInstancePerfWatchOptionsType struct {
	runtimeID string
	alias     string
	watch     []string
	log       string
	duration  time.Duration
}

var cmdInstancePerfWatchOptions cmdInstancePerfWatchOptionsType

var cmdInstancePerfWatch = &cobra.Command{
	Use:   "watch",
	Short: "Watch SKR resources and log their events with the changes made",
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := cmdInstancePerfWatchOptions

		watchSpecs := opts.watch
		if len(watchSpecs) == 0 {
			watchSpecs = []string{"*.cloud-resources.kyma-project.io"}
		}

		// Resolve the rest.Config of the cluster to watch.
		restConfig, err := resolvePerfWatchRestConfig(rootCtx, opts.runtimeID, opts.alias)
		if err != nil {
			return err
		}

		// Build dynamic client + discovery/RESTMapper.
		dyn, err := dynamic.NewForConfig(restConfig)
		if err != nil {
			return fmt.Errorf("failed to create dynamic client: %w", err)
		}
		disco, err := discovery.NewDiscoveryClientForConfig(restConfig)
		if err != nil {
			return fmt.Errorf("failed to create discovery client: %w", err)
		}
		groupResources, err := restmapper.GetAPIGroupResources(disco)
		if err != nil {
			return fmt.Errorf("failed to load API group resources: %w", err)
		}
		mapper := restmapper.NewDiscoveryRESTMapper(groupResources)

		// Resolve the watch specs into concrete GVR targets.
		targets, err := resolveWatchTargets(watchSpecs, disco, mapper)
		if err != nil {
			return err
		}
		if len(targets) == 0 {
			return fmt.Errorf("no resources matched the given watch specs: %v", watchSpecs)
		}

		// Set up the lifecycle context (CTRL+C via rootCtx, plus optional duration).
		ctx := rootCtx
		var cancel context.CancelFunc
		if opts.duration > 0 {
			ctx, cancel = context.WithTimeout(ctx, opts.duration)
			defer cancel()
		}

		// Open the full log sink if requested.
		sink, err := newEventSink(opts.log)
		if err != nil {
			return err
		}
		defer sink.Close()

		fmt.Printf("Watching %d resource kind(s):\n", len(targets))
		for _, t := range targets {
			fmt.Printf("  - %s\n", t.groupKind())
		}
		fmt.Println()

		w := &perfWatcher{
			dyn:      dyn,
			targets:  targets,
			sink:     sink,
			previous: map[string]*unstructured.Unstructured{},
		}
		w.printHeader()
		w.run(ctx)

		return nil
	},
}

func init() {
	cmdInstancePerf.AddCommand(cmdInstancePerfWatch)
	cmdInstancePerfWatch.Flags().StringVarP(&cmdInstancePerfWatchOptions.runtimeID, "runtime-id", "r", "", "Runtime ID of the SKR instance to watch")
	cmdInstancePerfWatch.Flags().StringVarP(&cmdInstancePerfWatchOptions.alias, "alias", "a", "", "Alias of the runtime to watch")
	cmdInstancePerfWatch.Flags().StringArrayVar(&cmdInstancePerfWatchOptions.watch, "watch", nil, "What to watch, e.g. 'IpRange.cloud-resources.kyma-project.io' or '*.cloud-resources.kyma-project.io' (default '*.cloud-resources.kyma-project.io')")
	cmdInstancePerfWatch.Flags().StringVar(&cmdInstancePerfWatchOptions.log, "log", "", "Full log destination file (multi-document YAML); off if not set")
	cmdInstancePerfWatch.Flags().DurationVar(&cmdInstancePerfWatchOptions.duration, "duration", 0, "Optional duration to watch for; runs until interrupted if not set")
	cmdInstancePerfWatch.MarkFlagsMutuallyExclusive("runtime-id", "alias")
}

// resolvePerfWatchRestConfig resolves the rest.Config of the cluster to watch.
// When neither runtimeID nor alias is given it falls back to the ambient kubeconfig context.
func resolvePerfWatchRestConfig(ctx context.Context, runtimeID, alias string) (*rest.Config, error) {
	if runtimeID == "" && alias == "" {
		rc, err := ctrl.GetConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to load current kubeconfig context: %w", err)
		}
		return rc, nil
	}

	keb, err := e2ekeb.Create(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create keb: %w", err)
	}

	id := runtimeID
	if id == "" {
		arr, err := keb.List(ctx, e2ekeb.WithAlias(alias))
		if err != nil {
			return nil, fmt.Errorf("failed to list instances by alias: %w", err)
		}
		if len(arr) == 0 {
			return nil, fmt.Errorf("instance %q not found", alias)
		}
		if len(arr) > 1 {
			return nil, fmt.Errorf("more than one instance found for alias %q", alias)
		}
		id = arr[0].RuntimeID
	}

	b, _, err := keb.GetInstanceKubeconfig(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance kubeconfig: %w", err)
	}
	cc, err := clientcmd.NewClientConfigFromBytes(b)
	if err != nil {
		return nil, fmt.Errorf("failed to create client config from kubeconfig: %w", err)
	}
	rc, err := cc.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get rest config from client config: %w", err)
	}
	return rc, nil
}

// watchTarget is one concrete resource kind to watch.
type watchTarget struct {
	gvr        schema.GroupVersionResource
	kind       string
	group      string
	namespaced bool
}

func (t watchTarget) groupKind() string {
	if t.group == "" {
		return t.kind
	}
	return t.kind + "." + t.group
}

// resolveWatchTargets turns the watch specs (kind.group or *.group) into concrete GVR targets.
func resolveWatchTargets(specs []string, disco discovery.DiscoveryInterface, mapper meta.RESTMapper) ([]watchTarget, error) {
	seen := map[schema.GroupVersionResource]bool{}
	var targets []watchTarget

	addMapping := func(kind, group string) error {
		m, err := mapper.RESTMapping(schema.GroupKind{Group: group, Kind: kind})
		if err != nil {
			return fmt.Errorf("failed to resolve %s.%s: %w", kind, group, err)
		}
		if seen[m.Resource] {
			return nil
		}
		seen[m.Resource] = true
		targets = append(targets, watchTarget{
			gvr:        m.Resource,
			kind:       kind,
			group:      group,
			namespaced: m.Scope.Name() == meta.RESTScopeNameNamespace,
		})
		return nil
	}

	for _, spec := range specs {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		idx := strings.Index(spec, ".")
		if idx < 0 {
			return nil, fmt.Errorf("invalid watch spec %q, expected 'Kind.group' or '*.group'", spec)
		}
		kind := spec[:idx]
		group := spec[idx+1:]
		if group == "" {
			return nil, fmt.Errorf("invalid watch spec %q, empty group", spec)
		}

		if kind == "*" {
			kinds, err := kindsInGroup(disco, group)
			if err != nil {
				return nil, err
			}
			for _, k := range kinds {
				if err := addMapping(k, group); err != nil {
					return nil, err
				}
			}
			continue
		}

		if err := addMapping(kind, group); err != nil {
			return nil, err
		}
	}

	return targets, nil
}

// kindsInGroup enumerates all resource kinds of the preferred version of the given API group.
func kindsInGroup(disco discovery.DiscoveryInterface, group string) ([]string, error) {
	groups, err := disco.ServerGroups()
	if err != nil {
		return nil, fmt.Errorf("failed to list server groups: %w", err)
	}
	var version string
	for _, g := range groups.Groups {
		if g.Name == group {
			version = g.PreferredVersion.Version
			break
		}
	}
	if version == "" {
		return nil, fmt.Errorf("API group %q not found on the server", group)
	}

	gv := schema.GroupVersion{Group: group, Version: version}.String()
	list, err := disco.ServerResourcesForGroupVersion(gv)
	if err != nil {
		return nil, fmt.Errorf("failed to list resources for %s: %w", gv, err)
	}

	kindSet := map[string]bool{}
	var kinds []string
	for _, r := range list.APIResources {
		if strings.Contains(r.Name, "/") { // skip subresources
			continue
		}
		if !canWatch(r.Verbs) {
			continue
		}
		if kindSet[r.Kind] {
			continue
		}
		kindSet[r.Kind] = true
		kinds = append(kinds, r.Kind)
	}
	sort.Strings(kinds)
	return kinds, nil
}

func canWatch(verbs metav1.Verbs) bool {
	for _, v := range verbs {
		if v == "watch" {
			return true
		}
	}
	return false
}

// perfWatcher owns the aggregated watch loop and per-resource state.
type perfWatcher struct {
	dyn      dynamic.Interface
	targets  []watchTarget
	sink     *eventSink
	previous map[string]*unstructured.Unstructured
}

type aggregatedEvent struct {
	target watchTarget
	event  watch.Event
}

func (w *perfWatcher) run(ctx context.Context) {
	events := make(chan aggregatedEvent, 128)
	var wg sync.WaitGroup
	for _, t := range w.targets {
		wg.Add(1)
		go func(t watchTarget) {
			defer wg.Done()
			w.watchTarget(ctx, t, events)
		}(t)
	}

	// Close the events channel once all watcher goroutines exit.
	go func() {
		wg.Wait()
		close(events)
	}()

	for {
		select {
		case <-ctx.Done():
			// Drain remaining buffered events best-effort, then stop.
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			w.handleEvent(ev)
		}
	}
}

// watchTarget runs a resilient watch for a single GVR, re-establishing it on channel close.
func (w *perfWatcher) watchTarget(ctx context.Context, t watchTarget, out chan<- aggregatedEvent) {
	var ri dynamic.ResourceInterface
	if t.namespaced {
		ri = w.dyn.Resource(t.gvr).Namespace(metav1.NamespaceAll)
	} else {
		ri = w.dyn.Resource(t.gvr)
	}

	resourceVersion := ""
	for {
		if ctx.Err() != nil {
			return
		}
		wi, err := ri.Watch(ctx, metav1.ListOptions{ResourceVersion: resourceVersion})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// Reset resourceVersion (may be too old / 410 Gone) and retry after a short backoff.
			resourceVersion = ""
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				continue
			}
		}

		for {
			select {
			case <-ctx.Done():
				wi.Stop()
				return
			case event, ok := <-wi.ResultChan():
				if !ok {
					// Channel closed by the apiserver; re-establish the watch.
					wi.Stop()
					goto reconnect
				}
				if event.Type == watch.Error {
					// Restart from scratch on error events.
					wi.Stop()
					resourceVersion = ""
					goto reconnect
				}
				if u, ok := event.Object.(*unstructured.Unstructured); ok {
					resourceVersion = u.GetResourceVersion()
				}
				select {
				case out <- aggregatedEvent{target: t, event: event}:
				case <-ctx.Done():
					wi.Stop()
					return
				}
			}
		}
	reconnect:
	}
}

// handleEvent processes one watch event: computes the diff, prints the console row, logs to the sink.
func (w *perfWatcher) handleEvent(ae aggregatedEvent) {
	u, ok := ae.event.Object.(*unstructured.Unstructured)
	if !ok {
		return
	}

	// Always drop managedFields before compare/log.
	stripManagedFields(u)

	key := ae.target.groupKind() + "/" + u.GetNamespace() + "/" + u.GetName()

	var diff map[string]any
	switch ae.event.Type {
	case watch.Added:
		diff = u.DeepCopy().Object
		w.previous[key] = u.DeepCopy()
	case watch.Modified:
		prev := w.previous[key]
		if prev == nil {
			diff = u.DeepCopy().Object
		} else {
			diff = diffUnstructured(prev.Object, u.Object)
			// Carry apiVersion/kind for context, matching the spec example.
			if diff != nil {
				if _, ok := diff["apiVersion"]; !ok {
					diff["apiVersion"] = u.GetAPIVersion()
				}
				if _, ok := diff["kind"]; !ok {
					diff["kind"] = u.GetKind()
				}
			}
		}
		w.previous[key] = u.DeepCopy()
	case watch.Deleted:
		diff = nil
		delete(w.previous, key)
	default:
		return
	}

	ts := time.Now().UTC().Format(time.RFC3339)
	w.printRow(ts, string(ae.event.Type), ae.target, u)
	if w.sink != nil {
		w.sink.Write(eventRecord{
			APIVersion:      u.GetAPIVersion(),
			Kind:            u.GetKind(),
			Event:           string(ae.event.Type),
			Timestamp:       ts,
			ResourceVersion: u.GetResourceVersion(),
			Generation:      u.GetGeneration(),
			Name:            u.GetName(),
			Namespace:       u.GetNamespace(),
			Diff:            diff,
		})
	}
}

func stripManagedFields(u *unstructured.Unstructured) {
	unstructured.RemoveNestedField(u.Object, "metadata", "managedFields")
}

// diffUnstructured returns a tree containing only the fields that differ between prev and cur.
// Fields present in prev but removed in cur are represented with a nil value.
func diffUnstructured(prev, cur map[string]any) map[string]any {
	out := map[string]any{}
	// Changed or added keys.
	for k, cv := range cur {
		pv, existed := prev[k]
		if !existed {
			out[k] = cv
			continue
		}
		if sub := diffValue(pv, cv); sub != nil {
			out[k] = sub.value
		}
	}
	// Removed keys.
	for k := range prev {
		if _, ok := cur[k]; !ok {
			out[k] = nil
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

type diffResult struct {
	value any
}

// diffValue compares two values; returns nil if equal, otherwise the minimal changed value.
func diffValue(pv, cv any) *diffResult {
	pm, pIsMap := pv.(map[string]any)
	cm, cIsMap := cv.(map[string]any)
	if pIsMap && cIsMap {
		sub := diffUnstructured(pm, cm)
		if sub == nil {
			return nil
		}
		return &diffResult{value: sub}
	}
	if reflect.DeepEqual(pv, cv) {
		return nil
	}
	return &diffResult{value: cv}
}

// ---- Console summary ----

func (w *perfWatcher) printHeader() {
	fmt.Printf("%-21s %-9s %-40s %-24s %-12s %-11s %-21s %s\n",
		"Timestamp", "Event", "GroupKind", "Name", "Namespace", "Generation", "DeletionTimestamp", "Conditions")
}

func (w *perfWatcher) printRow(ts, event string, t watchTarget, u *unstructured.Unstructured) {
	deletionTS := "-"
	if dt := u.GetDeletionTimestamp(); dt != nil {
		deletionTS = dt.UTC().Format(time.RFC3339)
	}
	fmt.Printf("%-21s %-9s %-40s %-24s %-12s %-11d %-21s %s\n",
		ts,
		event,
		t.groupKind(),
		shortOrDash(u.GetName()),
		shortOrDash(u.GetNamespace()),
		u.GetGeneration(),
		deletionTS,
		conditionsSummary(u),
	)
}

func shortOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// conditionsSummary renders status.conditions as "type,status,reason" tuples joined by ";".
func conditionsSummary(u *unstructured.Unstructured) string {
	conds, found, err := unstructured.NestedSlice(u.Object, "status", "conditions")
	if err != nil || !found || len(conds) == 0 {
		return "-"
	}
	var parts []string
	for _, c := range conds {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		t, _, _ := unstructured.NestedString(cm, "type")
		st, _, _ := unstructured.NestedString(cm, "status")
		reason, _, _ := unstructured.NestedString(cm, "reason")
		parts = append(parts, strings.Join([]string{t, st, reason}, ","))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ";")
}

// ---- Full log sink ----

type eventRecord struct {
	APIVersion      string         `json:"apiVersion"`
	Kind            string         `json:"kind"`
	Event           string         `json:"event"`
	Timestamp       string         `json:"timestamp"`
	ResourceVersion string         `json:"resourceVersion"`
	Generation      int64          `json:"generation"`
	Name            string         `json:"name"`
	Namespace       string         `json:"namespace"`
	Diff            map[string]any `json:"diff,omitempty"`
}

type eventSink struct {
	mu    sync.Mutex
	f     *os.File
	w     *bufio.Writer
	first bool
}

// newEventSink opens the given log file for writing; returns nil sink if filename is empty.
func newEventSink(filename string) (*eventSink, error) {
	if filename == "" {
		return nil, nil
	}
	f, err := os.Create(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file %q: %w", filename, err)
	}
	return &eventSink{
		f:     f,
		w:     bufio.NewWriter(f),
		first: true,
	}, nil
}

func (s *eventSink) Write(rec eventRecord) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	b, err := yaml.Marshal(rec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal event to yaml: %v\n", err)
		return
	}
	if !s.first {
		_, _ = s.w.WriteString("---\n")
	}
	s.first = false
	_, _ = s.w.Write(b)
	// Flush each event so the log is durable and tailable while the watch runs.
	_ = s.w.Flush()
}

func (s *eventSink) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.w.Flush()
	_ = s.f.Close()
}
