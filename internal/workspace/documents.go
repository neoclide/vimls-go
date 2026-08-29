package workspace

import (
	"context"
	"errors"
	"sort"
	"sync"

	"github.com/chemzqm/vimls-go/internal/text"
)

var (
	ErrDocumentNotOpen = errors.New("document is not open")
	ErrStaleVersion    = errors.New("document version is not newer")
)

type Analysis struct {
	Context        context.Context
	Snapshot       *text.Snapshot
	ConfigRevision uint64
}

type document struct {
	snapshot       *text.Snapshot
	analysisCancel context.CancelFunc
}

// Documents owns open document snapshots and their active analysis contexts.
type Documents struct {
	mu             sync.RWMutex
	documents      map[string]*document
	nextRevision   uint64
	configRevision uint64
}

func NewDocuments() *Documents {
	return &Documents{
		documents:      make(map[string]*document),
		configRevision: 1,
	}
}

func (d *Documents) Open(uri string, version int32, content string) *text.Snapshot {
	d.mu.Lock()
	defer d.mu.Unlock()
	if previous := d.documents[uri]; previous != nil {
		previous.cancelAnalysis()
	}
	d.nextRevision++
	snapshot := text.NewSnapshot(uri, d.nextRevision, &version, content)
	d.documents[uri] = &document{snapshot: snapshot}
	return snapshot
}

func (d *Documents) Change(uri string, version int32, encoding text.Encoding, changes []text.Change) (*text.Snapshot, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	current := d.documents[uri]
	if current == nil {
		return nil, ErrDocumentNotOpen
	}
	if previousVersion, ok := current.snapshot.Version(); ok && version <= previousVersion {
		return nil, ErrStaleVersion
	}
	nextRevision := d.nextRevision + 1
	snapshot, err := text.ApplyChanges(current.snapshot, nextRevision, &version, encoding, changes)
	if err != nil {
		return nil, err
	}
	current.cancelAnalysis()
	current.snapshot = snapshot
	d.nextRevision = nextRevision
	return snapshot, nil
}

func (d *Documents) Save(uri string, content *string) (*text.Snapshot, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	current := d.documents[uri]
	if current == nil {
		return nil, ErrDocumentNotOpen
	}
	if content == nil || *content == current.snapshot.Text() {
		return current.snapshot, nil
	}
	d.nextRevision++
	version, hasVersion := current.snapshot.Version()
	var versionPointer *int32
	if hasVersion {
		versionPointer = &version
	}
	current.cancelAnalysis()
	current.snapshot = text.NewSnapshot(uri, d.nextRevision, versionPointer, *content)
	return current.snapshot, nil
}

func (d *Documents) Close(uri string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	current := d.documents[uri]
	if current == nil {
		return false
	}
	current.cancelAnalysis()
	delete(d.documents, uri)
	return true
}

func (d *Documents) Snapshot(uri string) (*text.Snapshot, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	current := d.documents[uri]
	if current == nil {
		return nil, false
	}
	return current.snapshot, true
}

// Snapshots returns the current immutable open snapshots in URI lexical order.
// The returned slice is independent of the document store.
func (d *Documents) Snapshots() []*text.Snapshot {
	d.mu.RLock()
	snapshots := make([]*text.Snapshot, 0, len(d.documents))
	for _, current := range d.documents {
		snapshots = append(snapshots, current.snapshot)
	}
	d.mu.RUnlock()
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].URI() < snapshots[j].URI()
	})
	return snapshots
}

func (d *Documents) BeginAnalysis(ctx context.Context, uri string) (Analysis, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	current := d.documents[uri]
	if current == nil {
		return Analysis{}, false
	}
	current.cancelAnalysis()
	analysisCtx, cancel := context.WithCancel(ctx)
	current.analysisCancel = cancel
	return Analysis{
		Context:        analysisCtx,
		Snapshot:       current.snapshot,
		ConfigRevision: d.configRevision,
	}, true
}

func (d *Documents) IsCurrent(analysis Analysis) bool {
	if analysis.Context == nil || analysis.Context.Err() != nil || analysis.Snapshot == nil {
		return false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	current := d.documents[analysis.Snapshot.URI()]
	return current != nil && current.snapshot == analysis.Snapshot && d.configRevision == analysis.ConfigRevision
}

func (d *Documents) ConfigurationChanged() []*text.Snapshot {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.configRevision++
	snapshots := make([]*text.Snapshot, 0, len(d.documents))
	for _, current := range d.documents {
		current.cancelAnalysis()
		snapshots = append(snapshots, current.snapshot)
	}
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].URI() < snapshots[j].URI()
	})
	return snapshots
}

func (d *Documents) ConfigRevision() uint64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.configRevision
}

func (d *Documents) Len() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.documents)
}

func (d *document) cancelAnalysis() {
	if d.analysisCancel != nil {
		d.analysisCancel()
		d.analysisCancel = nil
	}
}
