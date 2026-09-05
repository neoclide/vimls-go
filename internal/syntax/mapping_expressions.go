package syntax

import "strings"

// parseMappingRegisterExpressions retains source-mapped expressions from Vim's
// interactive expression prompts. See Vim v9.2.1015 i_CTRL-R, c_CTRL-R,
// c_CTRL-\_e, quote_=, and @. Generated command text remains opaque.
func parseMappingRegisterExpressions(file *File, command *Command) bool {
	mapping := command.Mapping
	if mapping == nil || mapping.Expr || mapping.Query || mapping.Kind == MappingUnmap || mapping.RHS.Start >= mapping.RHS.End {
		return false
	}
	mode := mapping.Mode & MappingModeInsertCommandLine
	if mode == 0 {
		if mapping.Mode&MappingModeNormalVisualSelectOperator == 0 {
			return false
		}
		mode = MappingModeNormal
	}
	position, end := mapping.RHS.Start, mapping.RHS.End
	resumeInsert, found := false, false
	for position < end {
		key, size := mappingExpressionKeyAt(file.Source, position, end)
		start := -1
		executeRegister := false
		if mode == MappingModeNormal {
			switch key {
			case ":", "/", "?":
				mode = MappingModeCommandLine
			case "i", "I", "a", "A", "o", "O", "R", "s", "S":
				if mapping.Mode != MappingModeNormal && !resumeInsert {
					return found
				}
				mode = MappingModeInsert
				resumeInsert = false
			case "\"", "@":
				if position+size >= end || file.Source[position+size] != '=' {
					return found
				}
				start = position + size + 1
				executeRegister = key == "@"
			case "p", "P":
				if resumeInsert {
					mode, resumeInsert = MappingModeInsert, false
				}
			case "<esc>", "0", "1", "2", "3", "4", "5", "6", "7", "8", "9":
			default:
				// Do not infer mode changes through arbitrary Normal commands.
				return found
			}
		} else {
			switch key {
			case "<esc>", "<c-c>":
				return found
			case "<cr>":
				if mode&MappingModeCommandLine != 0 {
					if !resumeInsert {
						return found
					}
					mode, resumeInsert = MappingModeInsert, false
				}
			case "<cmd>", "<scriptcmd>":
				// These payloads execute directly, rather than typing a command line.
				position += size
				for position < end {
					next, nextSize := mappingExpressionKeyAt(file.Source, position, end)
					position += nextSize
					if next == "<cr>" {
						break
					}
				}
				continue
			case "<c-v>", "<c-q>":
				// A quoted control key does not open an expression prompt.
				position += size
				_, quoted := mappingExpressionKeyAt(file.Source, position, end)
				position += quoted
				continue
			case "<c-o>":
				if mode == MappingModeInsert {
					mode, resumeInsert = MappingModeNormal, true
				}
			case "<c-\\>":
				next, nextSize := mappingExpressionKeyAt(file.Source, position+size, end)
				if next == "e" && mode == MappingModeCommandLine {
					start = position + size + nextSize
				} else if next == "<c-o>" && mode == MappingModeInsert {
					mode, resumeInsert = MappingModeNormal, true
					size += nextSize
				} else {
					return found
				}
			case "<c-r>":
				next, nextSize := mappingExpressionKeyAt(file.Source, position+size, end)
				if next == "<c-r>" || next == "<c-o>" || next == "<c-p>" && mode == MappingModeInsert {
					size += nextSize
					next, nextSize = mappingExpressionKeyAt(file.Source, position+size, end)
				}
				if next == "=" {
					start = position + size + nextSize
				} else {
					// Consume the register name/object key, not the following text.
					size += nextSize
				}
			}
		}
		if start < 0 {
			position += size
			continue
		}
		found = true
		position = start
		opaque, cancelled := false, false
		for position < end {
			key, size = mappingExpressionKeyAt(file.Source, position, end)
			if key == "<cr>" || key == "<esc>" || key == "<c-c>" {
				cancelled = key != "<cr>"
				break
			}
			// Other special keys can edit or expand the prompt. They require a
			// decoded source view before its expression can be analyzed safely.
			opaque = opaque || len(key) > 1 && key[0] == '<' && key != "<sid>"
			position += size
		}
		if !opaque && !cancelled {
			expression, _ := parseExpressionWithVersion(file.Source[start:position], start, command.Dialect, command.ScriptVersion)
			if expression != nil {
				command.Expressions = append(command.Expressions, expression)
			}
		}
		if position < end {
			position += size
		}
		if executeRegister {
			// @= executes arbitrary generated keys, so its resulting mode is unknown.
			return found
		}
	}
	return found
}

// mappingExpressionKeyAt reads key notation without decoding or changing byte
// positions. Literal characters keep their case (Normal i and I differ).
func mappingExpressionKeyAt(source string, position, end int) (string, int) {
	if position >= end {
		return "", 0
	}
	key := source[position : position+1]
	size := 1
	if source[position] == '<' {
		for next := position + 1; next < end; next++ {
			if source[next] == '<' || isSpace(source[next]) {
				break
			}
			if source[next] == '>' {
				size = next + 1 - position
				key = strings.ToLower(source[position : next+1])
				break
			}
		}
	}
	switch key {
	case "\r", "\n", "<c-m>", "<c-j>", "<nl>", "<return>", "<enter>":
		key = "<cr>"
	case "\x1b", "<c-[>":
		key = "<esc>"
	case "\x03":
		key = "<c-c>"
	case "\x0f":
		key = "<c-o>"
	case "\x10":
		key = "<c-p>"
	case "\x11":
		key = "<c-q>"
	case "\x12":
		key = "<c-r>"
	case "\x16":
		key = "<c-v>"
	case "\x1c":
		key = "<c-\\>"
	}
	return key, size
}
