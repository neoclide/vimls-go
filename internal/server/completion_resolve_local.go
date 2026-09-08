package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"go.lsp.dev/protocol"
)

// Revision distinguishes edits and close/reopen, including identical content.
// Content also protects against resolving an item retained across server restarts.
type localCompletionTarget struct {
	URI      string      `json:"uri"`
	Revision uint64      `json:"revision"`
	Content  string      `json:"content"`
	Span     syntax.Span `json:"span"`
}

func localCompletionResolveData(snapshot *text.Snapshot, declaration *analysis.Declaration) []byte {
	data, _ := json.Marshal(completionResolveTarget{
		Kind: completionResolveLocal, Name: declaration.Name,
		Local: &localCompletionTarget{URI: snapshot.URI(), Revision: snapshot.Revision(),
			Content: fmt.Sprintf("%x", snapshot.ContentID()), Span: declaration.Span},
	})
	return data
}

func (s *Server) resolveLocalCompletion(ctx context.Context, item *protocol.CompletionItem, target completionResolveTarget) (*protocol.CompletionItem, error) {
	local := target.Local
	if local == nil {
		return item, nil
	}
	snapshot, ok := s.documents.Snapshot(local.URI)
	if !ok || snapshot.Revision() != local.Revision || fmt.Sprintf("%x", snapshot.ContentID()) != local.Content {
		return item, nil
	}
	finish := s.completionPriority.begin()
	defer finish()
	file := s.parseSnapshotContext(ctx, snapshot)
	if file == nil {
		if ctx.Err() != nil {
			return nil, protocol.ErrRequestCancelled
		}
		return item, nil
	}
	_, facts := s.completionSnapshotFacts(ctx, snapshot, file)
	if facts == nil {
		if ctx.Err() != nil {
			return nil, protocol.ErrRequestCancelled
		}
		return item, nil
	}
	if hook := s.testHooks.beforeLocalCompletionResolve; hook != nil {
		hook(snapshot)
	}
	resolved := *item
	for _, declaration := range facts.Declarations {
		if ctx.Err() != nil {
			return nil, protocol.ErrRequestCancelled
		}
		if declaration.Span != local.Span || declaration.Name != target.Name {
			continue
		}
		detail := string(declaration.Kind)
		typ := analysis.NewCompletionTypes(facts).DeclarationType(declaration)
		if typ.Name != "" && typ.Name != analysis.ValueTypeAny {
			detail += ": " + formatValueType(typ)
		}
		resolved.Detail = protocol.NewOptional(detail)
		break
	}
	if ctx.Err() != nil {
		return nil, protocol.ErrRequestCancelled
	}
	if current, ok := s.documents.Snapshot(local.URI); !ok || current != snapshot {
		return item, nil
	}
	return &resolved, nil
}
