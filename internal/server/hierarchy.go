package server

import (
	"context"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/neoclide/vimls-go/internal/analysis"
	"github.com/neoclide/vimls-go/internal/syntax"
	"github.com/neoclide/vimls-go/internal/text"
	"github.com/neoclide/vimls-go/internal/workspace"
	jsonrpc2 "go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

const (
	maxHierarchyItemData = 1024
	maxHierarchyResults  = 1000
)

type hierarchySymbol struct {
	fact     workspace.SymbolFact
	source   string
	snapshot *text.Snapshot
}

type hierarchyItemData struct {
	Version   uint8               `json:"v"`
	URI       string              `json:"u"`
	Kind      analysis.SymbolKind `json:"k"`
	Start     int                 `json:"s"`
	End       int                 `json:"e"`
	ContentID string              `json:"c"`
}

func (s *Server) PrepareTypeHierarchy(ctx context.Context, params *protocol.TypeHierarchyPrepareParams) ([]protocol.TypeHierarchyItem, error) {
	if err := s.waitForWorkspaceIndex(ctx); err != nil {
		return nil, err
	}
	for attempt := range 2 {
		snapshot, file, encoding, err := s.structureDocument(ctx, params.TextDocument.URI.String())
		if err != nil || snapshot == nil || file == nil {
			return []protocol.TypeHierarchyItem{}, err
		}
		offset, err := snapshot.Offset(fromProtocolPosition(params.Position), encoding)
		if err != nil {
			return []protocol.TypeHierarchyItem{}, s.structureCurrent(ctx, snapshot)
		}
		path, ok := workspaceURIPath(params.TextDocument.URI)
		if !ok {
			return []protocol.TypeHierarchyItem{}, s.structureCurrent(ctx, snapshot)
		}
		state := s.captureWorkspaceNavigationState()
		target, ok := s.typeHierarchyTargetAt(state, path, snapshot.Text(), file, offset)
		if ok {
			if sameWorkspacePath(target.fact.Path, path) {
				target.snapshot = snapshot
			}
			item, valid := typeHierarchyItem(target, encoding)
			if !valid {
				return []protocol.TypeHierarchyItem{}, s.structureCurrent(ctx, snapshot)
			}
			current, err := s.hierarchyCurrent(ctx, state, snapshot, target.snapshot)
			if err != nil {
				return nil, err
			}
			if current {
				return []protocol.TypeHierarchyItem{item}, nil
			}
		} else {
			current, err := s.hierarchyCurrent(ctx, state, snapshot)
			if err != nil {
				return nil, err
			}
			if current {
				return []protocol.TypeHierarchyItem{}, nil
			}
		}
		if attempt == 1 {
			return nil, protocol.ErrContentModified
		}
	}
	return nil, protocol.ErrContentModified
}

func (s *Server) Supertypes(ctx context.Context, params *protocol.TypeHierarchySupertypesParams) ([]protocol.TypeHierarchyItem, error) {
	if err := s.waitForWorkspaceIndex(ctx); err != nil {
		return nil, err
	}
	for attempt := range 2 {
		state := s.captureWorkspaceNavigationState()
		target, encoding, err := s.validateTypeHierarchyItem(params.Item, state)
		if err != nil {
			return nil, err
		}
		if target == nil {
			return []protocol.TypeHierarchyItem{}, nil
		}
		relations := s.typeRelationsForSymbol(state, *target)
		results := make([]hierarchySymbol, 0, len(relations))
		used := []*text.Snapshot{target.snapshot}
		for index, relation := range relations {
			if index%32 == 0 && ctx.Err() != nil {
				return nil, protocol.ErrRequestCancelled
			}
			parent, ok := s.hierarchyTargetAtSpan(state, relation.Fact.Child.Path, relation.Source, relation.Fact.ParentSpan)
			if !ok {
				continue
			}
			parent, ok = s.resolveAggregateAlias(state, parent, nil)
			if !ok || !validTypeRelation(target.fact.Kind, parent.fact.Kind, relation.Fact.Kind) {
				continue
			}
			results = append(results, parent)
			used = append(used, parent.snapshot)
		}
		items, err := typeHierarchyItems(results, encoding, s.hierarchyLimit)
		if err != nil {
			return nil, err
		}
		current, err := s.hierarchyCurrent(ctx, state, used...)
		if err != nil {
			return nil, err
		}
		if current {
			return items, nil
		}
		if attempt == 1 {
			return nil, protocol.ErrContentModified
		}
	}
	return nil, protocol.ErrContentModified
}

func (s *Server) Subtypes(ctx context.Context, params *protocol.TypeHierarchySubtypesParams) ([]protocol.TypeHierarchyItem, error) {
	if err := s.waitForWorkspaceIndex(ctx); err != nil {
		return nil, err
	}
	for attempt := range 2 {
		state := s.captureWorkspaceNavigationState()
		target, encoding, err := s.validateTypeHierarchyItem(params.Item, state)
		if err != nil {
			return nil, err
		}
		if target == nil {
			return []protocol.TypeHierarchyItem{}, nil
		}
		if !s.relationshipQueriesComplete(state) {
			return nil, hierarchyRequestFailed("workspace relationship index is incomplete")
		}
		candidates, snapshots, err := s.typeRelationCandidatesForTarget(state, *target)
		if err != nil {
			return nil, err
		}
		results := make([]hierarchySymbol, 0, len(candidates))
		used := append([]*text.Snapshot{target.snapshot}, snapshots...)
		for index, candidate := range candidates {
			if index%32 == 0 && ctx.Err() != nil {
				return nil, protocol.ErrRequestCancelled
			}
			parent, ok := s.hierarchyTargetAtSpan(state, candidate.Fact.Child.Path, candidate.Source, candidate.Fact.ParentSpan)
			if !ok {
				continue
			}
			parent, ok = s.resolveAggregateAlias(state, parent, nil)
			if !ok || parent.fact.Key() != target.fact.Key() {
				continue
			}
			child, ok := s.hierarchySymbolForKey(state, candidate.Fact.Child)
			if !ok || !validTypeRelation(child.fact.Kind, target.fact.Kind, candidate.Fact.Kind) {
				continue
			}
			results = append(results, child)
			used = append(used, child.snapshot, parent.snapshot)
		}
		items, err := typeHierarchyItems(results, encoding, s.hierarchyLimit)
		if err != nil {
			return nil, err
		}
		current, err := s.hierarchyCurrent(ctx, state, used...)
		if err != nil {
			return nil, err
		}
		if current {
			return items, nil
		}
		if attempt == 1 {
			return nil, protocol.ErrContentModified
		}
	}
	return nil, protocol.ErrContentModified
}

func (s *Server) Implementation(ctx context.Context, params *protocol.ImplementationParams) (protocol.DefinitionResult, error) {
	if err := s.waitForWorkspaceIndex(ctx); err != nil {
		return nil, err
	}
	for attempt := range 2 {
		snapshot, file, encoding, err := s.structureDocument(ctx, params.TextDocument.URI.String())
		if err != nil || snapshot == nil || file == nil {
			return protocol.LocationSlice{}, err
		}
		offset, err := snapshot.Offset(fromProtocolPosition(params.Position), encoding)
		if err != nil {
			return protocol.LocationSlice{}, s.structureCurrent(ctx, snapshot)
		}
		path, ok := workspaceURIPath(params.TextDocument.URI)
		if !ok {
			return protocol.LocationSlice{}, s.structureCurrent(ctx, snapshot)
		}
		state := s.captureWorkspaceNavigationState()
		target, ok := s.implementationTargetAt(state, path, snapshot.Text(), file, offset)
		if !ok {
			current, currentErr := s.hierarchyCurrent(ctx, state, snapshot)
			if currentErr != nil {
				return nil, currentErr
			}
			if current {
				return protocol.LocationSlice{}, nil
			}
			if attempt == 1 {
				return nil, protocol.ErrContentModified
			}
			continue
		}
		if sameWorkspacePath(target.fact.Path, path) {
			target.snapshot = snapshot
		}
		if implementationNeedsRelationships(target) && !s.relationshipQueriesComplete(state) {
			return nil, hierarchyRequestFailed("workspace relationship index is incomplete")
		}
		implementations, used, err := s.implementationsOf(ctx, state, target)
		if err != nil {
			return nil, err
		}
		locations := make(protocol.LocationSlice, 0, len(implementations))
		for _, implementation := range deduplicateHierarchySymbols(implementations) {
			itemSnapshot := text.NewSnapshot(uri.File(implementation.fact.Path).String(), 0, nil, implementation.source)
			if rangeValue, valid := protocolRange(itemSnapshot, encoding, implementation.fact.SelectionRange); valid {
				locations = append(locations, protocol.Location{URI: uri.File(implementation.fact.Path), Range: rangeValue})
			}
		}
		if len(locations) > s.hierarchyLimit {
			return nil, hierarchyRequestFailed("implementation result limit exceeded")
		}
		current, currentErr := s.hierarchyCurrent(ctx, state, append(used, snapshot, target.snapshot)...)
		if currentErr != nil {
			return nil, currentErr
		}
		if current {
			return locations, nil
		}
		if attempt == 1 {
			return nil, protocol.ErrContentModified
		}
	}
	return nil, protocol.ErrContentModified
}

func (s *Server) implementationTargetAt(state workspaceNavigationSnapshot, path, source string, file *syntax.File, offset int) (hierarchySymbol, bool) {
	facts := workspace.CollectSymbolFacts(path, file)
	for _, fact := range facts {
		if !spanContains(fact.SelectionRange, offset) {
			continue
		}
		switch fact.Kind {
		case analysis.SymbolKindClass, analysis.SymbolKindInterface, analysis.SymbolKindEnum, analysis.SymbolKindTypeAlias,
			analysis.SymbolKindMethod, analysis.SymbolKindVariable, analysis.SymbolKindConstant:
			target := hierarchySymbol{fact: fact, source: source}
			if fact.Kind == analysis.SymbolKindTypeAlias {
				return s.resolveAggregateAlias(state, target, nil)
			}
			return target, true
		}
	}
	result := analysis.Analyze(file)
	for _, reference := range result.References {
		if reference == nil || !spanContains(reference.Span, offset) || reference.Declaration == nil {
			continue
		}
		if fact, ok := localHierarchyDeclaration(facts, reference.Declaration); ok {
			target := hierarchySymbol{fact: fact, source: source}
			if fact.Kind == analysis.SymbolKindTypeAlias {
				return s.resolveAggregateAlias(state, target, nil)
			}
			return target, true
		}
	}
	external := workspace.CollectExternalReferencesFromAnalysis(path, file, result)
	for _, reference := range external {
		if spanContains(reference.Span, offset) {
			if target, ok := s.resolveWorkspaceReference(state, reference); ok {
				symbol := hierarchySymbolFromNavigationTarget(target)
				if symbol.fact.Kind == analysis.SymbolKindTypeAlias {
					return s.resolveAggregateAlias(state, symbol, nil)
				}
				return symbol, true
			}
		}
	}
	if member := memberExpressionAtOffset(file.Commands, offset); member != nil {
		if declaration, _, ok := memberNavigationSymbols(file, result, member); ok {
			if fact, found := localHierarchySymbol(facts, declaration); found {
				return hierarchySymbol{fact: fact, source: source}, true
			}
		}
		if len(member.Children) == 1 {
			if reference := importedAggregateReferenceForReceiver(path, file, result, member.Children[0], external); reference != nil {
				if base, ok := s.resolveWorkspaceReference(state, *reference); ok {
					if target, ok := s.resolveWorkspaceMemberTarget(base, member.Value, importedMemberClassReceiver(file, member.Children[0])); ok {
						return hierarchySymbolFromNavigationTarget(target), true
					}
				}
			}
		}
	}
	if target, ok := s.typeHierarchyTargetAt(state, path, source, file, offset); ok {
		return target, true
	}
	return hierarchySymbol{}, false
}

func (s *Server) implementationsOf(ctx context.Context, state workspaceNavigationSnapshot, target hierarchySymbol) ([]hierarchySymbol, []*text.Snapshot, error) {
	if aggregateHierarchyKind(target.fact.Kind) {
		switch target.fact.Kind {
		case analysis.SymbolKindInterface:
			descendants, used, err := s.descendantsOf(ctx, state, target)
			return descendants, used, err
		case analysis.SymbolKindClass:
			if !target.fact.Abstract {
				return nil, nil, nil
			}
			descendants, used, err := s.descendantsOf(ctx, state, target)
			if err != nil {
				return nil, used, err
			}
			concrete := descendants[:0]
			for _, descendant := range descendants {
				if descendant.fact.Kind == analysis.SymbolKindClass && !descendant.fact.Abstract {
					concrete = append(concrete, descendant)
				}
			}
			return concrete, used, nil
		default:
			return nil, nil, nil
		}
	}
	if target.fact.Kind != analysis.SymbolKindMethod && target.fact.Kind != analysis.SymbolKindVariable && target.fact.Kind != analysis.SymbolKindConstant {
		return nil, nil, nil
	}
	owner, ok := s.memberOwner(state, target)
	if !ok || !aggregateHierarchyKind(owner.fact.Kind) {
		return nil, nil, nil
	}
	descendants, used, err := s.descendantsOf(ctx, state, owner)
	if err != nil {
		return nil, used, err
	}
	providers := make([]hierarchySymbol, 0, len(descendants))
	for index, descendant := range descendants {
		if index%32 == 0 && ctx.Err() != nil {
			return nil, used, protocol.ErrRequestCancelled
		}
		var provider hierarchySymbol
		var found bool
		if owner.fact.Kind == analysis.SymbolKindInterface || target.fact.Abstract {
			provider, found = s.effectiveMemberProvider(state, descendant, target, make(map[workspace.SymbolKey]bool))
		} else {
			provider, found = s.directMemberProvider(descendant, target)
		}
		if found {
			providers = append(providers, provider)
			used = append(used, provider.snapshot)
		}
	}
	return providers, used, nil
}

func implementationNeedsRelationships(target hierarchySymbol) bool {
	if target.fact.Kind == analysis.SymbolKindInterface || target.fact.Kind == analysis.SymbolKindClass && target.fact.Abstract {
		return true
	}
	return target.fact.Kind == analysis.SymbolKindMethod || target.fact.Kind == analysis.SymbolKindVariable || target.fact.Kind == analysis.SymbolKindConstant
}

func (s *Server) descendantsOf(ctx context.Context, state workspaceNavigationSnapshot, target hierarchySymbol) ([]hierarchySymbol, []*text.Snapshot, error) {
	queue := []hierarchySymbol{target}
	seen := map[workspace.SymbolKey]bool{target.fact.Key(): true}
	result := make([]hierarchySymbol, 0)
	used := []*text.Snapshot{target.snapshot}
	for len(queue) > 0 {
		if ctx.Err() != nil {
			return nil, used, protocol.ErrRequestCancelled
		}
		current := queue[0]
		queue = queue[1:]
		candidates, snapshots, err := s.typeRelationCandidatesForTarget(state, current)
		if err != nil {
			return nil, used, err
		}
		used = append(used, snapshots...)
		for _, candidate := range candidates {
			parent, ok := s.hierarchyTargetAtSpan(state, candidate.Fact.Child.Path, candidate.Source, candidate.Fact.ParentSpan)
			if !ok {
				continue
			}
			parent, ok = s.resolveAggregateAlias(state, parent, nil)
			if !ok || parent.fact.Key() != current.fact.Key() {
				continue
			}
			child, ok := s.hierarchySymbolForKey(state, candidate.Fact.Child)
			if !ok || !validTypeRelation(child.fact.Kind, current.fact.Kind, candidate.Fact.Kind) || seen[child.fact.Key()] {
				continue
			}
			seen[child.fact.Key()] = true
			result = append(result, child)
			queue = append(queue, child)
			used = append(used, child.snapshot, parent.snapshot)
			if len(result) > s.hierarchyLimit {
				return nil, used, hierarchyRequestFailed("implementation result limit exceeded")
			}
		}
	}
	return result, used, nil
}

func (s *Server) memberOwner(_ workspaceNavigationSnapshot, member hierarchySymbol) (hierarchySymbol, bool) {
	if member.fact.OwnerSelectionRange.Start >= member.fact.OwnerSelectionRange.End {
		return hierarchySymbol{}, false
	}
	for _, fact := range workspace.CollectSymbolFacts(member.fact.Path, syntax.Parse(member.source)) {
		if fact.SelectionRange == member.fact.OwnerSelectionRange && aggregateHierarchyKind(fact.Kind) {
			return hierarchySymbol{fact: fact, source: member.source, snapshot: member.snapshot}, true
		}
	}
	return hierarchySymbol{}, false
}

func (s *Server) directMemberProvider(owner, expected hierarchySymbol) (hierarchySymbol, bool) {
	for _, fact := range workspace.CollectSymbolFacts(owner.fact.Path, syntax.Parse(owner.source)) {
		if fact.OwnerSelectionRange != owner.fact.SelectionRange || fact.Name != expected.fact.Name || !implementationMemberCategory(fact.Kind, expected.fact.Kind) || fact.Static != expected.fact.Static || fact.Abstract {
			continue
		}
		provider := hierarchySymbol{fact: fact, source: owner.source, snapshot: owner.snapshot}
		if compatibleMember(expected, provider) {
			return provider, true
		}
	}
	return hierarchySymbol{}, false
}

func (s *Server) effectiveMemberProvider(state workspaceNavigationSnapshot, owner, expected hierarchySymbol, seen map[workspace.SymbolKey]bool) (hierarchySymbol, bool) {
	if seen[owner.fact.Key()] {
		return hierarchySymbol{}, false
	}
	seen[owner.fact.Key()] = true
	if provider, ok := s.directMemberProvider(owner, expected); ok {
		return provider, true
	}
	for _, relation := range s.typeRelationsForSymbol(state, owner) {
		if relation.Fact.Kind != analysis.TypeRelationExtends {
			continue
		}
		parent, ok := s.hierarchyTargetAtSpan(state, relation.Fact.Child.Path, relation.Source, relation.Fact.ParentSpan)
		if !ok {
			continue
		}
		parent, ok = s.resolveAggregateAlias(state, parent, nil)
		if ok {
			if provider, found := s.effectiveMemberProvider(state, parent, expected, seen); found {
				return provider, true
			}
		}
	}
	return hierarchySymbol{}, false
}

func compatibleMember(expected, actual hierarchySymbol) bool {
	if !implementationMemberCategory(expected.fact.Kind, actual.fact.Kind) || expected.fact.Static != actual.fact.Static {
		return false
	}
	expectedFile := syntax.Parse(expected.source)
	actualFile := syntax.Parse(actual.source)
	expectedCommand := commandForSymbolSpan(expectedFile.Commands, expected.fact.SelectionRange)
	actualCommand := commandForSymbolSpan(actualFile.Commands, actual.fact.SelectionRange)
	if (expected.fact.Kind == analysis.SymbolKindVariable || expected.fact.Kind == analysis.SymbolKindConstant) && hierarchyCommandHasModifier(expectedCommand, "public") != hierarchyCommandHasModifier(actualCommand, "public") {
		return false
	}
	if expected.fact.Kind == analysis.SymbolKindMethod {
		return compatibleFunctionSignature(expectedCommand, actualCommand)
	}
	expectedAnalysis, actualAnalysis := analysis.Analyze(expectedFile), analysis.Analyze(actualFile)
	var expectedDeclaration, actualDeclaration *analysis.Declaration
	for _, declaration := range expectedAnalysis.Declarations {
		if declaration != nil && declaration.Span == expected.fact.SelectionRange {
			expectedDeclaration = declaration
			break
		}
	}
	for _, declaration := range actualAnalysis.Declarations {
		if declaration != nil && declaration.Span == actual.fact.SelectionRange {
			actualDeclaration = declaration
			break
		}
	}
	return expectedDeclaration != nil && actualDeclaration != nil && equalValueType(expectedDeclaration.Type, actualDeclaration.Type)
}

func implementationMemberCategory(left, right analysis.SymbolKind) bool {
	if left == analysis.SymbolKindMethod || right == analysis.SymbolKindMethod {
		return left == analysis.SymbolKindMethod && right == analysis.SymbolKindMethod
	}
	return (left == analysis.SymbolKindVariable || left == analysis.SymbolKindConstant) && (right == analysis.SymbolKindVariable || right == analysis.SymbolKindConstant)
}

func hierarchyCommandHasModifier(command *syntax.Command, name string) bool {
	if command == nil {
		return false
	}
	for _, modifier := range command.Modifiers {
		if modifier.Name == name {
			return true
		}
	}
	return false
}

func compatibleFunctionSignature(expectedCommand, actualCommand *syntax.Command) bool {
	if expectedCommand == nil || expectedCommand.Function == nil || actualCommand == nil || actualCommand.Function == nil {
		return false
	}
	expected, actual := expectedCommand.Function, actualCommand.Function
	if len(expected.TypeParameters) != len(actual.TypeParameters) || len(expected.Parameters) != len(actual.Parameters) {
		return false
	}
	typeParameters := make(map[string]string, len(expected.TypeParameters))
	for index := range expected.TypeParameters {
		typeParameters[expected.TypeParameters[index].Name] = actual.TypeParameters[index].Name
	}
	for index := range expected.Parameters {
		left, right := expected.Parameters[index], actual.Parameters[index]
		if left.Variadic != right.Variadic || (left.Default != nil) != (right.Default != nil) || !equalSyntaxType(left.Type, right.Type, typeParameters) {
			return false
		}
	}
	return equalSyntaxType(expected.ReturnType, actual.ReturnType, typeParameters)
}

func equalSyntaxType(left, right *syntax.Type, typeParameters map[string]string) bool {
	if left == nil || right == nil {
		return left == right
	}
	leftName := left.Name
	if mapped, ok := typeParameters[leftName]; ok {
		leftName = mapped
	}
	if left.Kind != right.Kind || leftName != right.Name || left.ArgumentCountKnown != right.ArgumentCountKnown || len(left.Arguments) != len(right.Arguments) {
		return false
	}
	for index := range left.Arguments {
		if !equalSyntaxType(left.Arguments[index], right.Arguments[index], typeParameters) {
			return false
		}
	}
	return equalSyntaxType(left.ReturnType, right.ReturnType, typeParameters)
}

func equalValueType(left, right analysis.ValueType) bool {
	if left.Name == "" || right.Name == "" {
		return false
	}
	if left.Name == analysis.ValueTypeAny {
		return true
	}
	if right.Name == analysis.ValueTypeAny {
		return false
	}
	if left.Name == "float" && right.Name == "number" || left.Name == "bool" && right.Name == "number" {
		return true
	}
	if left.Name != right.Name || left.ArgumentCountKnown != right.ArgumentCountKnown || left.RequiredArguments != right.RequiredArguments || left.Variadic != right.Variadic || len(left.Arguments) != len(right.Arguments) || (left.Return == nil) != (right.Return == nil) {
		return false
	}
	for index := range left.Arguments {
		if !equalValueType(left.Arguments[index], right.Arguments[index]) {
			return false
		}
	}
	return left.Return == nil || equalValueType(*left.Return, *right.Return)
}

func (s *Server) PrepareCallHierarchy(ctx context.Context, params *protocol.CallHierarchyPrepareParams) ([]protocol.CallHierarchyItem, error) {
	if err := s.waitForWorkspaceIndex(ctx); err != nil {
		return nil, err
	}
	for attempt := range 2 {
		snapshot, file, encoding, err := s.structureDocument(ctx, params.TextDocument.URI.String())
		if err != nil || snapshot == nil || file == nil {
			return []protocol.CallHierarchyItem{}, err
		}
		offset, err := snapshot.Offset(fromProtocolPosition(params.Position), encoding)
		if err != nil {
			return []protocol.CallHierarchyItem{}, s.structureCurrent(ctx, snapshot)
		}
		path, ok := workspaceURIPath(params.TextDocument.URI)
		if !ok {
			return []protocol.CallHierarchyItem{}, s.structureCurrent(ctx, snapshot)
		}
		state := s.captureWorkspaceNavigationState()
		target, ok := s.callHierarchyTargetAt(state, path, snapshot.Text(), file, offset)
		if ok {
			if sameWorkspacePath(target.fact.Path, path) {
				target.snapshot = snapshot
			}
			item, valid := callHierarchyItem(target, encoding)
			if !valid {
				return []protocol.CallHierarchyItem{}, s.structureCurrent(ctx, snapshot)
			}
			current, currentErr := s.hierarchyCurrent(ctx, state, snapshot, target.snapshot)
			if currentErr != nil {
				return nil, currentErr
			}
			if current {
				return []protocol.CallHierarchyItem{item}, nil
			}
		} else {
			current, currentErr := s.hierarchyCurrent(ctx, state, snapshot)
			if currentErr != nil {
				return nil, currentErr
			}
			if current {
				return []protocol.CallHierarchyItem{}, nil
			}
		}
		if attempt == 1 {
			return nil, protocol.ErrContentModified
		}
	}
	return nil, protocol.ErrContentModified
}

func (s *Server) OutgoingCalls(ctx context.Context, params *protocol.CallHierarchyOutgoingCallsParams) ([]protocol.CallHierarchyOutgoingCall, error) {
	if err := s.waitForWorkspaceIndex(ctx); err != nil {
		return nil, err
	}
	for attempt := range 2 {
		state := s.captureWorkspaceNavigationState()
		target, encoding, err := s.validateCallHierarchyItem(params.Item, state)
		if err != nil {
			return nil, err
		}
		if target == nil {
			return []protocol.CallHierarchyOutgoingCall{}, nil
		}
		facts := s.callFactsForSymbol(state, *target)
		type callGroup struct {
			target hierarchySymbol
			ranges []protocol.Range
		}
		groups := make(map[workspace.SymbolKey]*callGroup)
		used := []*text.Snapshot{target.snapshot}
		callerSnapshot := text.NewSnapshot(uri.File(target.fact.Path).String(), 0, nil, target.source)
		for index, fact := range facts {
			if index%32 == 0 && ctx.Err() != nil {
				return nil, protocol.ErrRequestCancelled
			}
			callee, ok := s.hierarchyTargetAtSpan(state, fact.Fact.Caller.Path, fact.Source, fact.Fact.CalleeSpan)
			if !ok || !callHierarchyKind(callee.fact.Kind) {
				continue
			}
			rangeValue, ok := protocolRange(callerSnapshot, encoding, fact.Fact.CalleeSpan)
			if !ok {
				continue
			}
			group := groups[callee.fact.Key()]
			if group == nil {
				group = &callGroup{target: callee}
				groups[callee.fact.Key()] = group
			}
			group.ranges = append(group.ranges, rangeValue)
			used = append(used, callee.snapshot)
		}
		if len(groups) > s.hierarchyLimit {
			return nil, hierarchyRequestFailed("call hierarchy result limit exceeded")
		}
		ordered := make([]*callGroup, 0, len(groups))
		for _, group := range groups {
			ordered = append(ordered, group)
		}
		sort.SliceStable(ordered, func(left, right int) bool { return hierarchySymbolLess(ordered[left].target, ordered[right].target) })
		result := make([]protocol.CallHierarchyOutgoingCall, 0, len(ordered))
		for _, group := range ordered {
			item, ok := callHierarchyItem(group.target, encoding)
			if !ok {
				continue
			}
			group.ranges = deduplicateRanges(group.ranges)
			result = append(result, protocol.CallHierarchyOutgoingCall{To: item, FromRanges: group.ranges})
		}
		current, currentErr := s.hierarchyCurrent(ctx, state, used...)
		if currentErr != nil {
			return nil, currentErr
		}
		if current {
			return result, nil
		}
		if attempt == 1 {
			return nil, protocol.ErrContentModified
		}
	}
	return nil, protocol.ErrContentModified
}

func (s *Server) IncomingCalls(ctx context.Context, params *protocol.CallHierarchyIncomingCallsParams) ([]protocol.CallHierarchyIncomingCall, error) {
	if err := s.waitForWorkspaceIndex(ctx); err != nil {
		return nil, err
	}
	for attempt := range 2 {
		state := s.captureWorkspaceNavigationState()
		target, encoding, err := s.validateCallHierarchyItem(params.Item, state)
		if err != nil {
			return nil, err
		}
		if target == nil {
			return []protocol.CallHierarchyIncomingCall{}, nil
		}
		if !s.relationshipQueriesComplete(state) {
			return nil, hierarchyRequestFailed("workspace relationship index is incomplete")
		}
		candidateNames := []string{target.fact.Name}
		trimmedName := strings.TrimPrefix(target.fact.Name, "g:")
		if trimmedName != target.fact.Name {
			candidateNames = append(candidateNames, trimmedName)
		}
		if target.fact.Exported {
			if autoloadName, ok := workspaceAutoloadName(target.fact.Path, target.fact.Name, state.roots); ok {
				candidateNames = append(candidateNames, autoloadName)
			}
		}
		var candidates []workspace.CallMatch
		var snapshots []*text.Snapshot
		seenCandidates := make(map[workspace.CallFact]bool)
		for _, name := range candidateNames {
			matches, used := s.callCandidates(state, name)
			snapshots = append(snapshots, used...)
			for _, match := range matches {
				if !seenCandidates[match.Fact] {
					seenCandidates[match.Fact] = true
					candidates = append(candidates, match)
				}
			}
		}
		type callGroup struct {
			caller hierarchySymbol
			ranges []protocol.Range
		}
		groups := make(map[workspace.SymbolKey]*callGroup)
		used := append([]*text.Snapshot{target.snapshot}, snapshots...)
		for index, candidate := range candidates {
			if index%32 == 0 && ctx.Err() != nil {
				return nil, protocol.ErrRequestCancelled
			}
			callee, ok := s.hierarchyTargetAtSpan(state, candidate.Fact.Caller.Path, candidate.Source, candidate.Fact.CalleeSpan)
			if !ok || callee.fact.Key() != target.fact.Key() {
				continue
			}
			caller, ok := s.hierarchySymbolForKey(state, candidate.Fact.Caller)
			if !ok || !callHierarchyKind(caller.fact.Kind) {
				continue
			}
			callerSnapshot := text.NewSnapshot(uri.File(caller.fact.Path).String(), 0, nil, caller.source)
			rangeValue, ok := protocolRange(callerSnapshot, encoding, candidate.Fact.CalleeSpan)
			if !ok {
				continue
			}
			group := groups[caller.fact.Key()]
			if group == nil {
				group = &callGroup{caller: caller}
				groups[caller.fact.Key()] = group
			}
			group.ranges = append(group.ranges, rangeValue)
			used = append(used, callee.snapshot, caller.snapshot)
		}
		if len(groups) > s.hierarchyLimit {
			return nil, hierarchyRequestFailed("call hierarchy result limit exceeded")
		}
		ordered := make([]*callGroup, 0, len(groups))
		for _, group := range groups {
			ordered = append(ordered, group)
		}
		sort.SliceStable(ordered, func(left, right int) bool { return hierarchySymbolLess(ordered[left].caller, ordered[right].caller) })
		result := make([]protocol.CallHierarchyIncomingCall, 0, len(ordered))
		for _, group := range ordered {
			item, ok := callHierarchyItem(group.caller, encoding)
			if !ok {
				continue
			}
			group.ranges = deduplicateRanges(group.ranges)
			result = append(result, protocol.CallHierarchyIncomingCall{From: item, FromRanges: group.ranges})
		}
		current, currentErr := s.hierarchyCurrent(ctx, state, used...)
		if currentErr != nil {
			return nil, currentErr
		}
		if current {
			return result, nil
		}
		if attempt == 1 {
			return nil, protocol.ErrContentModified
		}
	}
	return nil, protocol.ErrContentModified
}

func (s *Server) callHierarchyTargetAt(state workspaceNavigationSnapshot, path, source string, file *syntax.File, offset int) (hierarchySymbol, bool) {
	facts := workspace.CollectSymbolFacts(path, file)
	for _, fact := range facts {
		if spanContains(fact.SelectionRange, offset) && callHierarchyKind(fact.Kind) {
			return hierarchySymbol{fact: fact, source: source}, true
		}
	}
	if deferredCallHierarchyOffset(file, offset) {
		return hierarchySymbol{}, false
	}
	result := analysis.Analyze(file)
	for _, reference := range result.References {
		if reference == nil || !spanContains(reference.Span, offset) || reference.Declaration == nil {
			continue
		}
		if fact, ok := localHierarchyDeclaration(facts, reference.Declaration); ok && callHierarchyKind(fact.Kind) {
			return hierarchySymbol{fact: fact, source: source}, true
		}
	}
	external := workspace.CollectExternalReferencesFromAnalysis(path, file, result)
	for _, reference := range external {
		if spanContains(reference.Span, offset) {
			if target, ok := s.resolveWorkspaceReference(state, reference); ok && callHierarchyKind(target.match.Fact.Kind) {
				return hierarchySymbolFromNavigationTarget(target), true
			}
		}
	}
	if member := memberExpressionAtOffset(file.Commands, offset); member != nil {
		if symbol, _, ok := memberSymbolForStaticReceiver(file, result, member); ok {
			if fact, found := localHierarchySymbol(facts, symbol); found && callHierarchyKind(fact.Kind) {
				return hierarchySymbol{fact: fact, source: source}, true
			}
		}
		if len(member.Children) == 1 {
			if reference := importedAggregateReferenceForReceiver(path, file, result, member.Children[0], external); reference != nil {
				if base, ok := s.resolveWorkspaceReference(state, *reference); ok {
					if target, ok := s.resolveWorkspaceMemberTarget(base, member.Value, importedMemberClassReceiver(file, member.Children[0])); ok && callHierarchyKind(target.match.Fact.Kind) {
						return hierarchySymbolFromNavigationTarget(target), true
					}
				}
			}
		}
	}
	var enclosing *workspace.SymbolFact
	for index := range facts {
		fact := &facts[index]
		if !callHierarchyKind(fact.Kind) || !spanContains(fact.Range, offset) {
			continue
		}
		if enclosing == nil || fact.Range.End-fact.Range.Start < enclosing.Range.End-enclosing.Range.Start {
			enclosing = fact
		}
	}
	if enclosing == nil {
		return hierarchySymbol{}, false
	}
	return hierarchySymbol{fact: *enclosing, source: source}, true
}

func deferredCallHierarchyOffset(file *syntax.File, offset int) bool {
	deferred := false
	walkCommands(file.Commands, func(command *syntax.Command) {
		if deferred || !spanContains(command.Span, offset) {
			return
		}
		if command.Mapping != nil || command.Autocmd != nil || command.UserCommand != nil || command.Canonical == "autocmd" || command.Canonical == "command" {
			deferred = true
			return
		}
		walkCommandExpressions(command, func(expression *syntax.Expression) {
			if expression.Kind == syntax.ExpressionLambda && spanContains(expression.Span, offset) {
				deferred = true
			}
		})
	})
	return deferred
}

func (s *Server) callFactsForSymbol(state workspaceNavigationSnapshot, target hierarchySymbol) []workspace.CallMatch {
	if target.snapshot != nil {
		file := s.parseSnapshot(target.snapshot)
		analysisResult := analysis.Analyze(file)
		facts := workspace.CollectCallFactsFromAnalysis(target.fact.Path, file, analysisResult)
		result := make([]workspace.CallMatch, 0, len(facts))
		for _, fact := range facts {
			if fact.Caller == target.fact.Key() {
				result = append(result, workspace.CallMatch{Fact: fact, Source: target.source})
			}
		}
		return result
	}
	if state.index == nil {
		return nil
	}
	return state.index.Calls(target.fact.Key())
}

func (s *Server) callCandidates(state workspaceNavigationSnapshot, name string) ([]workspace.CallMatch, []*text.Snapshot) {
	openPaths := make(map[string]bool)
	snapshots := make([]*text.Snapshot, 0)
	result := make([]workspace.CallMatch, 0)
	for _, snapshot := range s.documents.Snapshots() {
		path, ok := workspaceURIPath(uri.URI(snapshot.URI()))
		if !ok {
			continue
		}
		openPaths[path] = true
		snapshots = append(snapshots, snapshot)
		if snapshot.ByteLen() > maxFileBytes {
			continue
		}
		file := s.parseSnapshot(snapshot)
		for _, fact := range workspace.CollectCallFactsFromAnalysis(path, file, analysis.Analyze(file)) {
			if fact.CalleeName == name {
				result = append(result, workspace.CallMatch{Fact: fact, Source: snapshot.Text()})
			}
		}
	}
	if state.index != nil {
		for _, candidate := range state.index.CallCandidates(name) {
			if !openPaths[candidate.Fact.Caller.Path] {
				result = append(result, candidate)
			}
		}
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].Fact.Caller.Path != result[right].Fact.Caller.Path {
			return result[left].Fact.Caller.Path < result[right].Fact.Caller.Path
		}
		return result[left].Fact.CalleeSpan.Start < result[right].Fact.CalleeSpan.Start
	})
	return result, snapshots
}

func callHierarchyItem(symbol hierarchySymbol, encoding text.Encoding) (protocol.CallHierarchyItem, bool) {
	snapshot := text.NewSnapshot(uri.File(symbol.fact.Path).String(), 0, nil, symbol.source)
	rangeValue, rangeOK := protocolRange(snapshot, encoding, symbol.fact.Range)
	selection, selectionOK := protocolRange(snapshot, encoding, symbol.fact.SelectionRange)
	if !rangeOK || !selectionOK {
		return protocol.CallHierarchyItem{}, false
	}
	data, err := protocol.Marshal(hierarchyItemData{
		Version: 1, URI: uri.File(symbol.fact.Path).String(), Kind: symbol.fact.Kind,
		Start: symbol.fact.SelectionRange.Start, End: symbol.fact.SelectionRange.End,
		ContentID: hierarchyContentID(symbol.source),
	})
	if err != nil {
		return protocol.CallHierarchyItem{}, false
	}
	detail := symbol.fact.Signature
	if detail == "" {
		detail = symbol.fact.Detail
	}
	return protocol.CallHierarchyItem{
		Name: symbol.fact.Name, Kind: protocolSymbolKind(symbol.fact.Kind), Detail: &detail,
		URI: uri.File(symbol.fact.Path), Range: rangeValue, SelectionRange: selection, Data: protocol.LSPAny(data),
	}, true
}

func (s *Server) validateCallHierarchyItem(item protocol.CallHierarchyItem, state workspaceNavigationSnapshot) (*hierarchySymbol, text.Encoding, error) {
	s.mu.Lock()
	encoding := s.encoding
	s.mu.Unlock()
	target, malformed, err := s.validateHierarchyData(item.Data, item.URI, item.Name, item.Kind, item.Range, item.SelectionRange, state, encoding)
	if err != nil || malformed || !callHierarchyKind(target.fact.Kind) {
		return nil, encoding, err
	}
	return &target, encoding, nil
}

func callHierarchyKind(kind analysis.SymbolKind) bool {
	return kind == analysis.SymbolKindFunction || kind == analysis.SymbolKindMethod || kind == analysis.SymbolKindConstructor
}

func hierarchySymbolLess(left, right hierarchySymbol) bool {
	leftKey, rightKey := left.fact.Key(), right.fact.Key()
	if leftKey.Path != rightKey.Path {
		return leftKey.Path < rightKey.Path
	}
	if leftKey.SelectionRange.Start != rightKey.SelectionRange.Start {
		return leftKey.SelectionRange.Start < rightKey.SelectionRange.Start
	}
	return leftKey.SelectionRange.End < rightKey.SelectionRange.End
}

func deduplicateRanges(ranges []protocol.Range) []protocol.Range {
	sort.SliceStable(ranges, func(left, right int) bool {
		if ranges[left].Start.Line != ranges[right].Start.Line {
			return ranges[left].Start.Line < ranges[right].Start.Line
		}
		if ranges[left].Start.Character != ranges[right].Start.Character {
			return ranges[left].Start.Character < ranges[right].Start.Character
		}
		if ranges[left].End.Line != ranges[right].End.Line {
			return ranges[left].End.Line < ranges[right].End.Line
		}
		return ranges[left].End.Character < ranges[right].End.Character
	})
	result := ranges[:0]
	for _, rangeValue := range ranges {
		if len(result) == 0 || result[len(result)-1] != rangeValue {
			result = append(result, rangeValue)
		}
	}
	return result
}

func (s *Server) typeHierarchyTargetAt(state workspaceNavigationSnapshot, path, source string, file *syntax.File, offset int) (hierarchySymbol, bool) {
	facts := workspace.CollectSymbolFacts(path, file)
	for _, fact := range facts {
		if spanContains(fact.SelectionRange, offset) && typeHierarchyKind(fact.Kind) {
			return s.resolveAggregateAlias(state, hierarchySymbol{fact: fact, source: source}, nil)
		}
	}
	for _, relation := range workspace.CollectTypeRelationFacts(path, file) {
		if spanContains(relation.ParentSpan, offset) {
			target, ok := s.hierarchyTargetAtSpan(state, path, source, relation.ParentSpan)
			if !ok {
				return hierarchySymbol{}, false
			}
			return s.resolveAggregateAlias(state, target, nil)
		}
	}
	result := analysis.Analyze(file)
	for _, reference := range result.References {
		if reference == nil || !spanContains(reference.Span, offset) || reference.Declaration == nil {
			continue
		}
		if target, ok := localHierarchyDeclaration(facts, reference.Declaration); ok {
			return s.resolveAggregateAlias(state, hierarchySymbol{fact: target, source: source}, nil)
		}
	}
	for _, reference := range workspace.CollectExternalReferencesFromAnalysis(path, file, result) {
		if !spanContains(reference.Span, offset) {
			continue
		}
		if target, ok := s.resolveWorkspaceReference(state, reference); ok {
			return s.resolveAggregateAlias(state, hierarchySymbolFromNavigationTarget(target), nil)
		}
	}
	return hierarchySymbol{}, false
}

func (s *Server) hierarchyTargetAtSpan(state workspaceNavigationSnapshot, path, source string, span syntax.Span) (hierarchySymbol, bool) {
	currentSource, snapshot, ok := s.hierarchySource(state, path)
	if ok {
		source = currentSource
	}
	if span.Start < 0 || span.Start >= span.End || span.End > len(source) {
		return hierarchySymbol{}, false
	}
	file := syntax.Parse(source)
	facts := workspace.CollectSymbolFacts(path, file)
	for _, fact := range facts {
		if fact.SelectionRange == span {
			return hierarchySymbol{fact: fact, source: source, snapshot: snapshot}, true
		}
	}
	result := analysis.Analyze(file)
	for _, reference := range result.References {
		if reference == nil || reference.Span != span || reference.Declaration == nil {
			continue
		}
		if target, ok := localHierarchyDeclaration(facts, reference.Declaration); ok {
			return hierarchySymbol{fact: target, source: source, snapshot: snapshot}, true
		}
	}
	external := workspace.CollectExternalReferencesFromAnalysis(path, file, result)
	for _, reference := range external {
		if reference.Span == span {
			if target, ok := s.resolveWorkspaceReference(state, reference); ok {
				return hierarchySymbolFromNavigationTarget(target), true
			}
		}
	}
	if member := memberExpressionAtOffset(file.Commands, span.Start); member != nil && (syntax.Span{Start: member.Operator.End, End: member.Span.End}) == span {
		if declaration, definition, ok := memberNavigationSymbols(file, result, member); ok {
			symbol := definition
			if symbol == nil {
				symbol = declaration
			}
			if fact, found := localHierarchySymbol(facts, symbol); found {
				return hierarchySymbol{fact: fact, source: source, snapshot: snapshot}, true
			}
		}
		if len(member.Children) == 1 {
			if reference := importedAggregateReferenceForReceiver(path, file, result, member.Children[0], external); reference != nil {
				if base, ok := s.resolveWorkspaceReference(state, *reference); ok {
					if target, ok := s.resolveWorkspaceMemberTarget(base, member.Value, importedMemberClassReceiver(file, member.Children[0])); ok {
						return hierarchySymbolFromNavigationTarget(target), true
					}
				}
			}
		}
	}
	if target, ok := s.resolveTypeName(state, path, source, file, span); ok {
		target.snapshot = snapshot
		return target, true
	}
	return hierarchySymbol{}, false
}

func (s *Server) resolveTypeName(state workspaceNavigationSnapshot, path, source string, file *syntax.File, span syntax.Span) (hierarchySymbol, bool) {
	raw := strings.TrimSpace(file.Text(span))
	if raw == "" {
		return hierarchySymbol{}, false
	}
	if dot := strings.IndexByte(raw, '.'); dot > 0 && dot < len(raw)-1 {
		alias, name := raw[:dot], raw[strings.LastIndexByte(raw, '.')+1:]
		var found *syntax.Import
		walkCommands(file.Commands, func(command *syntax.Command) {
			if command.Import == nil || command.Import.PathSpan.End > span.Start || workspace.ImportAlias(file, command.Import) != alias {
				return
			}
			if found == nil {
				found = command.Import
			} else {
				found = &syntax.Import{}
			}
		})
		if found == nil || found.PathSpan.Start >= found.PathSpan.End {
			return hierarchySymbol{}, false
		}
		reference := workspace.ExternalReferenceFact{
			Path: path, Name: name, Span: span, Kind: workspace.ExternalReferenceImportMember,
			ImportPath: file.Text(found.PathSpan), ImportAutoload: found.Autoload,
		}
		if target, ok := s.resolveWorkspaceReference(state, reference); ok {
			return hierarchySymbolFromNavigationTarget(target), true
		}
		return hierarchySymbol{}, false
	}
	var match *workspace.SymbolFact
	for _, fact := range workspace.CollectSymbolFacts(path, file) {
		if fact.Name != raw || !typeHierarchyKind(fact.Kind) {
			continue
		}
		if match != nil && match.Key() != fact.Key() {
			return hierarchySymbol{}, false
		}
		copy := fact
		match = &copy
	}
	if match == nil {
		return hierarchySymbol{}, false
	}
	return hierarchySymbol{fact: *match, source: source}, true
}

func (s *Server) resolveAggregateAlias(state workspaceNavigationSnapshot, target hierarchySymbol, seen map[workspace.SymbolKey]bool) (hierarchySymbol, bool) {
	if aggregateHierarchyKind(target.fact.Kind) {
		return target, true
	}
	if target.fact.Kind != analysis.SymbolKindTypeAlias {
		return hierarchySymbol{}, false
	}
	if seen == nil {
		seen = make(map[workspace.SymbolKey]bool)
	}
	key := target.fact.Key()
	if seen[key] {
		return hierarchySymbol{}, false
	}
	seen[key] = true
	file := syntax.Parse(target.source)
	var alias *syntax.TypeAlias
	walkCommands(file.Commands, func(command *syntax.Command) {
		if alias == nil && command.TypeAlias != nil && command.TypeAlias.Name == target.fact.SelectionRange {
			alias = command.TypeAlias
		}
	})
	if alias == nil || alias.Type == nil || alias.Type.Kind != syntax.TypeNamed {
		return hierarchySymbol{}, false
	}
	next, ok := s.hierarchyTargetAtSpan(state, target.fact.Path, target.source, alias.TypeSpan)
	if !ok {
		return hierarchySymbol{}, false
	}
	return s.resolveAggregateAlias(state, next, seen)
}

func (s *Server) hierarchySymbolForKey(state workspaceNavigationSnapshot, key workspace.SymbolKey) (hierarchySymbol, bool) {
	source, snapshot, ok := s.hierarchySource(state, key.Path)
	if !ok {
		return hierarchySymbol{}, false
	}
	for _, fact := range workspace.CollectSymbolFacts(key.Path, syntax.Parse(source)) {
		if fact.Key() == key {
			return hierarchySymbol{fact: fact, source: source, snapshot: snapshot}, true
		}
	}
	return hierarchySymbol{}, false
}

func (s *Server) hierarchySource(state workspaceNavigationSnapshot, path string) (string, *text.Snapshot, bool) {
	s.publishMu.Lock()
	snapshot, _, open := s.openWorkspaceSnapshotLocked(path)
	s.publishMu.Unlock()
	if open {
		return snapshot.Text(), snapshot, true
	}
	if state.index == nil {
		return "", nil, false
	}
	source, ok := state.index.Source(path)
	return source, nil, ok
}

func (s *Server) typeRelationsForSymbol(state workspaceNavigationSnapshot, target hierarchySymbol) []workspace.TypeRelationMatch {
	if target.snapshot != nil {
		facts := workspace.CollectTypeRelationFacts(target.fact.Path, syntax.Parse(target.source))
		result := make([]workspace.TypeRelationMatch, 0, len(facts))
		for _, fact := range facts {
			if fact.Child == target.fact.Key() {
				result = append(result, workspace.TypeRelationMatch{Fact: fact, Source: target.source})
			}
		}
		return result
	}
	if state.index == nil {
		return nil
	}
	return state.index.TypeRelations(target.fact.Key())
}

func (s *Server) typeRelationCandidates(state workspaceNavigationSnapshot, name string) ([]workspace.TypeRelationMatch, []*text.Snapshot) {
	openPaths := make(map[string]bool)
	snapshots := make([]*text.Snapshot, 0)
	result := make([]workspace.TypeRelationMatch, 0)
	for _, snapshot := range s.documents.Snapshots() {
		path, ok := workspaceURIPath(uri.URI(snapshot.URI()))
		if !ok {
			continue
		}
		openPaths[path] = true
		snapshots = append(snapshots, snapshot)
		if snapshot.ByteLen() > maxFileBytes {
			continue
		}
		for _, fact := range workspace.CollectTypeRelationFacts(path, s.parseSnapshot(snapshot)) {
			if fact.ParentName == name {
				result = append(result, workspace.TypeRelationMatch{Fact: fact, Source: snapshot.Text()})
			}
		}
	}
	if state.index != nil {
		for _, candidate := range state.index.TypeRelationCandidates(name) {
			if !openPaths[candidate.Fact.Child.Path] {
				result = append(result, candidate)
			}
		}
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].Fact.Child.Path != result[right].Fact.Child.Path {
			return result[left].Fact.Child.Path < result[right].Fact.Child.Path
		}
		return result[left].Fact.ParentSpan.Start < result[right].Fact.ParentSpan.Start
	})
	return result, snapshots
}

func (s *Server) typeRelationCandidatesForTarget(state workspaceNavigationSnapshot, target hierarchySymbol) ([]workspace.TypeRelationMatch, []*text.Snapshot, error) {
	names := []string{target.fact.Name}
	seenNames := map[string]bool{target.fact.Name: true}
	seenAliases := make(map[workspace.SymbolKey]bool)
	seenRelations := make(map[workspace.TypeRelationFact]bool)
	result := make([]workspace.TypeRelationMatch, 0)
	used := make([]*text.Snapshot, 0)
	for len(names) > 0 {
		name := names[0]
		names = names[1:]
		relations, snapshots := s.typeRelationCandidates(state, name)
		used = append(used, snapshots...)
		for _, relation := range relations {
			if !seenRelations[relation.Fact] {
				seenRelations[relation.Fact] = true
				result = append(result, relation)
			}
		}
		aliases, snapshots := s.typeAliasCandidates(state, name)
		used = append(used, snapshots...)
		for _, candidate := range aliases {
			if seenAliases[candidate.Fact.Alias] {
				continue
			}
			seenAliases[candidate.Fact.Alias] = true
			resolved, ok := s.hierarchyTargetAtSpan(state, candidate.Fact.Alias.Path, candidate.Source, candidate.Fact.TargetSpan)
			if !ok {
				continue
			}
			resolved, ok = s.resolveAggregateAlias(state, resolved, nil)
			if !ok || resolved.fact.Key() != target.fact.Key() {
				continue
			}
			used = append(used, resolved.snapshot)
			if !seenNames[candidate.Fact.AliasName] {
				seenNames[candidate.Fact.AliasName] = true
				names = append(names, candidate.Fact.AliasName)
				if len(seenNames) > s.hierarchyLimit {
					return nil, used, hierarchyRequestFailed("type alias result limit exceeded")
				}
			}
		}
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].Fact.Child.Path != result[right].Fact.Child.Path {
			return result[left].Fact.Child.Path < result[right].Fact.Child.Path
		}
		return result[left].Fact.ParentSpan.Start < result[right].Fact.ParentSpan.Start
	})
	return result, used, nil
}

func (s *Server) typeAliasCandidates(state workspaceNavigationSnapshot, name string) ([]workspace.TypeAliasMatch, []*text.Snapshot) {
	openPaths := make(map[string]bool)
	snapshots := make([]*text.Snapshot, 0)
	result := make([]workspace.TypeAliasMatch, 0)
	for _, snapshot := range s.documents.Snapshots() {
		path, ok := workspaceURIPath(uri.URI(snapshot.URI()))
		if !ok {
			continue
		}
		openPaths[path] = true
		snapshots = append(snapshots, snapshot)
		if snapshot.ByteLen() > maxFileBytes {
			continue
		}
		for _, fact := range workspace.CollectTypeAliasFacts(path, s.parseSnapshot(snapshot)) {
			if fact.TargetName == name {
				result = append(result, workspace.TypeAliasMatch{Fact: fact, Source: snapshot.Text()})
			}
		}
	}
	if state.index != nil {
		for _, candidate := range state.index.TypeAliasCandidates(name) {
			if !openPaths[candidate.Fact.Alias.Path] {
				result = append(result, candidate)
			}
		}
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].Fact.Alias.Path != result[right].Fact.Alias.Path {
			return result[left].Fact.Alias.Path < result[right].Fact.Alias.Path
		}
		return result[left].Fact.Alias.SelectionRange.Start < result[right].Fact.Alias.SelectionRange.Start
	})
	return result, snapshots
}

func typeHierarchyItem(symbol hierarchySymbol, encoding text.Encoding) (protocol.TypeHierarchyItem, bool) {
	snapshot := text.NewSnapshot(uri.File(symbol.fact.Path).String(), 0, nil, symbol.source)
	rangeValue, rangeOK := protocolRange(snapshot, encoding, symbol.fact.Range)
	selection, selectionOK := protocolRange(snapshot, encoding, symbol.fact.SelectionRange)
	if !rangeOK || !selectionOK {
		return protocol.TypeHierarchyItem{}, false
	}
	data, err := protocol.Marshal(hierarchyItemData{
		Version: 1, URI: uri.File(symbol.fact.Path).String(), Kind: symbol.fact.Kind,
		Start: symbol.fact.SelectionRange.Start, End: symbol.fact.SelectionRange.End,
		ContentID: hierarchyContentID(symbol.source),
	})
	if err != nil {
		return protocol.TypeHierarchyItem{}, false
	}
	detail := symbol.fact.Detail
	return protocol.TypeHierarchyItem{
		Name: symbol.fact.Name, Kind: protocolSymbolKind(symbol.fact.Kind), Detail: &detail,
		URI: uri.File(symbol.fact.Path), Range: rangeValue, SelectionRange: selection, Data: protocol.LSPAny(data),
	}, true
}

func typeHierarchyItems(symbols []hierarchySymbol, encoding text.Encoding, limit int) ([]protocol.TypeHierarchyItem, error) {
	symbols = deduplicateHierarchySymbols(symbols)
	if len(symbols) > limit {
		return nil, hierarchyRequestFailed("type hierarchy result limit exceeded")
	}
	items := make([]protocol.TypeHierarchyItem, 0, len(symbols))
	for _, symbol := range symbols {
		if item, ok := typeHierarchyItem(symbol, encoding); ok {
			items = append(items, item)
		}
	}
	return items, nil
}

func (s *Server) validateTypeHierarchyItem(item protocol.TypeHierarchyItem, state workspaceNavigationSnapshot) (*hierarchySymbol, text.Encoding, error) {
	s.mu.Lock()
	encoding := s.encoding
	s.mu.Unlock()
	target, malformed, err := s.validateHierarchyData(item.Data, item.URI, item.Name, item.Kind, item.Range, item.SelectionRange, state, encoding)
	if err != nil || malformed || !aggregateHierarchyKind(target.fact.Kind) {
		return nil, encoding, err
	}
	return &target, encoding, nil
}

func (s *Server) validateHierarchyData(raw protocol.LSPAny, itemURI uri.URI, name string, kind protocol.SymbolKind, itemRange, selection protocol.Range, state workspaceNavigationSnapshot, encoding text.Encoding) (hierarchySymbol, bool, error) {
	if len(raw) == 0 || len(raw) > maxHierarchyItemData {
		return hierarchySymbol{}, true, nil
	}
	var data hierarchyItemData
	if err := protocol.Unmarshal(raw, &data); err != nil || data.Version != 1 || data.URI != itemURI.String() || data.Start < 0 || data.Start >= data.End {
		return hierarchySymbol{}, true, nil
	}
	path, ok := workspaceURIPath(itemURI)
	if !ok || uri.File(path) != itemURI {
		return hierarchySymbol{}, true, nil
	}
	decoded, err := hex.DecodeString(data.ContentID)
	if err != nil || len(decoded) != len(text.ContentID{}) {
		return hierarchySymbol{}, true, nil
	}
	source, snapshot, ok := s.hierarchySource(state, path)
	if !ok {
		return hierarchySymbol{}, false, protocol.ErrContentModified
	}
	contentID := text.ContentIDOf(source)
	if !strings.EqualFold(data.ContentID, hex.EncodeToString(contentID[:])) {
		return hierarchySymbol{}, false, protocol.ErrContentModified
	}
	key := workspace.SymbolKey{Path: path, SelectionRange: syntax.Span{Start: data.Start, End: data.End}, Kind: data.Kind}
	target, ok := s.hierarchySymbolForKey(state, key)
	if !ok {
		return hierarchySymbol{}, true, nil
	}
	target.snapshot = snapshot
	checkSnapshot := text.NewSnapshot(itemURI.String(), 0, nil, source)
	rangeValue, rangeOK := protocolRange(checkSnapshot, encoding, target.fact.Range)
	selectionValue, selectionOK := protocolRange(checkSnapshot, encoding, target.fact.SelectionRange)
	if !rangeOK || !selectionOK || target.fact.Name != name || protocolSymbolKind(target.fact.Kind) != kind || rangeValue != itemRange || selectionValue != selection {
		return hierarchySymbol{}, true, nil
	}
	return target, false, nil
}

func (s *Server) hierarchyCurrent(ctx context.Context, state workspaceNavigationSnapshot, snapshots ...*text.Snapshot) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, protocol.ErrRequestCancelled
	}
	if hook := s.beforeWorkspaceIdentityCheck; hook != nil {
		hook()
	}
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	seen := make(map[*text.Snapshot]bool)
	for _, snapshot := range snapshots {
		if snapshot == nil || seen[snapshot] {
			continue
		}
		seen[snapshot] = true
		current, ok := s.documents.Snapshot(snapshot.URI())
		if !ok || current != snapshot {
			return false, protocol.ErrContentModified
		}
	}
	s.workspaceMu.Lock()
	current := s.workspaceIdentityCurrentLocked(state.identity)
	s.workspaceMu.Unlock()
	return current, nil
}

func (s *Server) relationshipQueriesComplete(state workspaceNavigationSnapshot) bool {
	if state.index == nil || !state.index.RelationshipsComplete() {
		return false
	}
	for _, snapshot := range s.documents.Snapshots() {
		if _, ok := workspaceURIPath(uri.URI(snapshot.URI())); ok && snapshot.ByteLen() > maxFileBytes {
			return false
		}
	}
	return true
}

func hierarchySymbolFromNavigationTarget(target workspaceNavigationTarget) hierarchySymbol {
	return hierarchySymbol{fact: target.match.Fact, source: target.match.Source, snapshot: target.openSnapshot}
}

func localHierarchyDeclaration(facts []workspace.SymbolFact, declaration *analysis.Declaration) (workspace.SymbolFact, bool) {
	if declaration == nil {
		return workspace.SymbolFact{}, false
	}
	for _, fact := range facts {
		if fact.SelectionRange == declaration.Span && fact.Kind == declaration.Kind && fact.Name == declaration.Name {
			return fact, true
		}
	}
	return workspace.SymbolFact{}, false
}

func localHierarchySymbol(facts []workspace.SymbolFact, symbol *analysis.Symbol) (workspace.SymbolFact, bool) {
	if symbol == nil {
		return workspace.SymbolFact{}, false
	}
	for _, fact := range facts {
		if fact.SelectionRange == symbol.SelectionRange && fact.Kind == symbol.Kind && fact.Name == symbol.Name {
			return fact, true
		}
	}
	return workspace.SymbolFact{}, false
}

func typeHierarchyKind(kind analysis.SymbolKind) bool {
	return aggregateHierarchyKind(kind) || kind == analysis.SymbolKindTypeAlias
}

func aggregateHierarchyKind(kind analysis.SymbolKind) bool {
	return kind == analysis.SymbolKindClass || kind == analysis.SymbolKindInterface || kind == analysis.SymbolKindEnum
}

func validTypeRelation(child, parent analysis.SymbolKind, kind analysis.TypeRelationKind) bool {
	switch kind {
	case analysis.TypeRelationExtends:
		return child == analysis.SymbolKindClass && parent == analysis.SymbolKindClass || child == analysis.SymbolKindInterface && parent == analysis.SymbolKindInterface
	case analysis.TypeRelationImplements:
		return (child == analysis.SymbolKindClass || child == analysis.SymbolKindEnum) && parent == analysis.SymbolKindInterface
	default:
		return false
	}
}

func deduplicateHierarchySymbols(symbols []hierarchySymbol) []hierarchySymbol {
	sort.SliceStable(symbols, func(left, right int) bool {
		leftKey, rightKey := symbols[left].fact.Key(), symbols[right].fact.Key()
		if leftKey.Path != rightKey.Path {
			return leftKey.Path < rightKey.Path
		}
		if leftKey.SelectionRange.Start != rightKey.SelectionRange.Start {
			return leftKey.SelectionRange.Start < rightKey.SelectionRange.Start
		}
		return leftKey.SelectionRange.End < rightKey.SelectionRange.End
	})
	result := symbols[:0]
	for _, symbol := range symbols {
		if len(result) == 0 || result[len(result)-1].fact.Key() != symbol.fact.Key() {
			result = append(result, symbol)
		}
	}
	return result
}

func hierarchyRequestFailed(message string) error {
	return jsonrpc2.NewError(jsonrpc2.Code(protocol.LSPErrorCodesRequestFailed), message)
}

func hierarchyContentID(source string) string {
	contentID := text.ContentIDOf(source)
	return hex.EncodeToString(contentID[:])
}
