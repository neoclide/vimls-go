package syntax

// EditorContext describes the editors in which a syntax node may execute.
// The zero value makes no assumption. Contradictory conditions are retained,
// but do not license suppressing diagnostics in their bodies.
type EditorContext uint8

const (
	EditorUnknown EditorContext = iota
	EditorNeovim
	EditorVim
	EditorUnreachable
)

// Each bit excludes one editor: intersection accumulates exclusions, while
// union keeps only exclusions shared by both alternatives.
func (context EditorContext) intersect(other EditorContext) EditorContext { return context | other }
func (context EditorContext) union(other EditorContext) EditorContext     { return context & other }

type editorCondition struct {
	yes, no    EditorContext
	boolean    bool
	incomplete bool
}

type editorContextAnnotator struct {
	conditions map[*Expression]editorCondition
	visited    map[*Expression]bool
}

// Run after command structure, expression parsing and source mapping have
// settled. Only the public parser entry points run this pass, so temporary
// embedded files receive the owning node's context once, after source mapping.
func annotateEditorContexts(file *File) {
	annotator := newEditorContextAnnotator()
	annotator.commands(file.Commands, file.Blocks, EditorUnknown)
}

func newEditorContextAnnotator() *editorContextAnnotator {
	return &editorContextAnnotator{
		conditions: make(map[*Expression]editorCondition),
		visited:    make(map[*Expression]bool),
	}
}

func (a *editorContextAnnotator) commands(commands []Command, blocks []Block, entry EditorContext) {
	type branchState struct {
		entry, body, remaining EditorContext
		nextBranch             int
	}
	states := make([]branchState, len(blocks))
	for index := range commands {
		command := &commands[index]
		context := entry
		if command.Block >= 0 && command.Block < len(blocks) {
			block := &blocks[command.Block]
			state := &states[command.Block]
			switch {
			case index == block.Header:
				if block.Parent >= 0 && block.Parent < len(states) {
					context = states[block.Parent].body
				}
				state.entry, state.body, state.remaining = context, context, context
				if block.Kind == BlockIf && !command.detailsOpaque {
					condition := a.commandCondition(command)
					state.body = context.intersect(condition.yes)
					state.remaining = context.intersect(condition.no)
				}
			case index == block.End:
				context = state.entry
			case block.Kind == BlockIf && state.nextBranch < len(block.Branches) && block.Branches[state.nextBranch] == index:
				state.nextBranch++
				context = state.remaining
				if command.Canonical == "else" {
					state.body, state.remaining = context, EditorUnreachable
				} else {
					condition := a.commandCondition(command)
					state.body = context.intersect(condition.yes)
					state.remaining = context.intersect(condition.no)
				}
			default:
				context = state.body
			}
		}
		command.EditorContext = context
		for _, expression := range command.Expressions {
			a.expression(expression, context)
		}
		for _, expression := range command.Targets {
			a.expression(expression, context)
		}
		if declaration := command.Declaration; declaration != nil {
			a.expression(declaration.Target, context)
			a.expression(declaration.Initializer, context)
		}
		if function := command.Function; function != nil {
			for _, parameter := range function.Parameters {
				a.expression(parameter.Default, context)
			}
		}
		for _, value := range command.EnumValues {
			a.expression(value.Initializer, context)
			for _, argument := range value.Arguments {
				a.expression(argument, context)
			}
		}
		if command.Import != nil {
			a.expression(command.Import.Path, context)
		}
		if command.For != nil {
			a.expression(command.For.Iterable, context)
		}
		if command.Mapping != nil {
			a.expression(command.Mapping.RHSExpression, context)
		}
		if command.Substitute != nil {
			a.expression(command.Substitute.Expression, context)
		}
		if command.Embedded != nil {
			a.commands(command.Embedded.Commands, command.Embedded.Blocks, context)
		}
	}
}

func (a *editorContextAnnotator) commandCondition(command *Command) editorCondition {
	if command.detailsOpaque || len(command.Expressions) != 1 {
		return editorCondition{}
	}
	return a.condition(command.Expressions[0])
}

func (a *editorContextAnnotator) expression(expression *Expression, context EditorContext) {
	if expression == nil || a.visited[expression] {
		return
	}
	a.visited[expression] = true
	expression.EditorContext = context
	for index, child := range expression.Children {
		childContext := context
		if len(expression.Children) == 2 && expression.Kind == ExpressionBinary && index == 1 && (expression.Value == "&&" || expression.Value == "||") {
			condition := a.condition(expression.Children[0])
			switch expression.Value {
			case "&&":
				childContext = context.intersect(condition.yes)
			case "||":
				childContext = context.intersect(condition.no)
			}
		} else if expression.Kind == ExpressionTernary && len(expression.Children) == 3 {
			condition := a.condition(expression.Children[0])
			if index == 1 {
				childContext = context.intersect(condition.yes)
			}
			if index == 2 {
				childContext = context.intersect(condition.no)
			}
		}
		a.expression(child, childContext)
	}
	for _, parameter := range expression.Parameters {
		a.expression(parameter.Default, context)
	}
	if expression.LambdaBody != nil {
		a.commands(expression.LambdaBody.Commands, expression.LambdaBody.Blocks, context)
	}
}

func (a *editorContextAnnotator) condition(expression *Expression) editorCondition {
	if expression == nil {
		return editorCondition{}
	}
	if condition, ok := a.conditions[expression]; ok {
		return condition
	}
	condition := editorCondition{}
	switch expression.Kind {
	case ExpressionMissing:
		condition.incomplete = true
	case ExpressionParenthesized:
		if len(expression.Children) == 1 {
			condition = a.condition(expression.Children[0])
		}
	case ExpressionCall:
		if len(expression.Children) == 2 {
			callee, argument := expression.Children[0], expression.Children[1]
			if callee != nil && callee.Kind == ExpressionIdentifier && callee.Value == "has" &&
				argument != nil && argument.Kind == ExpressionString && (argument.Value == "'nvim'" || argument.Value == "\"nvim\"") {
				condition = editorCondition{yes: EditorNeovim, no: EditorVim, boolean: true}
			}
		}
	case ExpressionNumber, ExpressionIdentifier:
		switch expression.Value {
		case "0", "v:false":
			condition = editorCondition{yes: EditorUnreachable, boolean: true}
		case "1", "v:true":
			condition = editorCondition{no: EditorUnreachable, boolean: true}
		}
	case ExpressionUnary:
		if expression.Value == "!" && len(expression.Children) == 1 {
			child := a.condition(expression.Children[0])
			condition = editorCondition{yes: child.no, no: child.yes, boolean: true}
		}
	case ExpressionBinary:
		if len(expression.Children) == 2 {
			left, right := a.condition(expression.Children[0]), a.condition(expression.Children[1])
			switch expression.Value {
			case "&&":
				condition = editorCondition{yes: left.yes.intersect(right.yes), no: left.no.union(left.yes.intersect(right.no)), boolean: true}
			case "||":
				condition = editorCondition{yes: left.yes.union(left.no.intersect(right.yes)), no: left.no.intersect(right.no), boolean: true}
			case "==", "==#", "==?", "!=", "!=#", "!=?":
				if left.boolean && right.boolean {
					condition = editorCondition{
						yes: left.yes.intersect(right.yes).union(left.no.intersect(right.no)),
						no:  left.yes.intersect(right.no).union(left.no.intersect(right.yes)), boolean: true,
					}
					if expression.Value[0] == '!' {
						condition.yes, condition.no = condition.no, condition.yes
					}
				}
			}
		}
	}
	for _, child := range expression.Children {
		if a.condition(child).incomplete {
			condition = editorCondition{incomplete: true}
			break
		}
	}
	a.conditions[expression] = condition
	return condition
}
