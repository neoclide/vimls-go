package benchlegacy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/chemzqm/vimls-go/internal/syntax"
	"github.com/chemzqm/vimls-go/internal/workspace"
	oldast "github.com/vim-jp/go-vimlparser/ast"
	oldparser "github.com/vim-jp/go-vimlparser/go"
)

type repeatedPaths []string

func (paths *repeatedPaths) String() string {
	return strings.Join(*paths, ",")
}

func (paths *repeatedPaths) Set(value string) error {
	*paths = append(*paths, value)
	return nil
}

var (
	benchmarkRoots    repeatedPaths
	benchmarkManifest string
	benchmarkWarmups  int

	vimlsSink        []*syntax.File
	goVimlparserSink []oldast.Node

	corpusOnce      sync.Once
	corpusCache     *legacyCorpus
	corpusError     error
	corpusLogOnce   sync.Once
	vimlsWarmupOnce sync.Once
	oldWarmupOnce   sync.Once
	looseWarmupOnce sync.Once
	batchWarmupOnce [3]sync.Once
	allWarmupOnce   [3]sync.Once
)

func init() {
	flag.Var(&benchmarkRoots, "root", "root file or directory to scan recursively; repeatable")
	flag.StringVar(&benchmarkManifest, "manifest", "", "newline-delimited file manifest")
	flag.IntVar(&benchmarkWarmups, "warmup", 2, "untimed corpus passes before each benchmark")
}

type inputFile struct {
	path   string
	source string
	lines  []string
}

type legacyCorpus struct {
	all              []inputFile
	legacy           []inputFile
	common           []inputFile
	totalBytes       int64
	legacyBytes      int64
	commonBytes      int64
	vim9Files        int
	vimlsErrorFiles  int
	vimlsDiagnostics int
	oldErrorFiles    int
	hash             string
	vimlsExamples    []string
	oldExamples      []string
	vimlsOutput      vimlsOutputCounts
	oldOutput        oldOutputCounts
}

type vimlsOutputCounts struct {
	commands       int
	structured     int
	expressions    int
	tokens         int
	blocks         int
	comments       int
	textBodies     int
	textLines      int
	highlights     int
	highlightAttrs int
	syntaxCommands int
	syntaxKeywords int
	syntaxOptions  int
	syntaxPatterns int
	setCommands    int
	setOptions     int
	substitutes    int
	subExprs       int
}

type oldOutputCounts struct {
	nodes       int
	statements  int
	typed       int
	opaque      int
	expressions int
	comments    int
}

func BenchmarkLegacyParsers(b *testing.B) {
	corpusOnce.Do(func() {
		corpusCache, corpusError = loadLegacyCorpus(benchmarkRoots, benchmarkManifest)
	})
	if corpusError != nil {
		b.Fatal(corpusError)
	}
	corpus := corpusCache
	if len(corpus.common) == 0 {
		b.Fatal("common clean legacy corpus is empty")
	}
	corpusLogOnce.Do(func() {
		b.Logf(
			"corpus sha256=%s discovered=%d/%dB legacy=%d/%dB vim9_excluded=%d common=%d/%dB vimls_error_files=%d vimls_diagnostics=%d go_vimlparser_error_files=%d",
			corpus.hash, len(corpus.all), corpus.totalBytes, len(corpus.legacy), corpus.legacyBytes,
			corpus.vim9Files, len(corpus.common), corpus.commonBytes, corpus.vimlsErrorFiles,
			corpus.vimlsDiagnostics, corpus.oldErrorFiles,
		)
		b.Logf(
			"common output vimls_go commands=%d structured=%d expressions=%d tokens=%d blocks=%d comments=%d text_bodies=%d text_lines=%d highlights=%d highlight_attributes=%d syntax_commands=%d syntax_keywords=%d syntax_options=%d syntax_patterns=%d set_commands=%d set_options=%d substitutes=%d substitute_expressions=%d; go_vimlparser nodes=%d statements=%d typed=%d opaque=%d expressions=%d comments=%d",
			corpus.vimlsOutput.commands, corpus.vimlsOutput.structured, corpus.vimlsOutput.expressions,
			corpus.vimlsOutput.tokens, corpus.vimlsOutput.blocks, corpus.vimlsOutput.comments,
			corpus.vimlsOutput.textBodies, corpus.vimlsOutput.textLines, corpus.vimlsOutput.highlights,
			corpus.vimlsOutput.highlightAttrs, corpus.vimlsOutput.syntaxCommands,
			corpus.vimlsOutput.syntaxKeywords, corpus.vimlsOutput.syntaxOptions,
			corpus.vimlsOutput.syntaxPatterns, corpus.vimlsOutput.setCommands,
			corpus.vimlsOutput.setOptions, corpus.vimlsOutput.substitutes,
			corpus.vimlsOutput.subExprs, corpus.oldOutput.nodes,
			corpus.oldOutput.statements, corpus.oldOutput.typed, corpus.oldOutput.opaque,
			corpus.oldOutput.expressions, corpus.oldOutput.comments,
		)
		for _, example := range corpus.vimlsExamples {
			b.Logf("vimls diagnostic example: %s", example)
		}
		for _, example := range corpus.oldExamples {
			b.Logf("go-vimlparser error example: %s", example)
		}
	})

	b.Run("vimls-go-common", func(b *testing.B) {
		benchmarkVimls(b, corpus.common, corpus.commonBytes, &vimlsWarmupOnce)
	})
	b.Run("go-vimlparser-common", func(b *testing.B) {
		benchmarkOldParser(b, corpus.common, corpus.commonBytes)
	})
	b.Run("vimls-go-loose-all", func(b *testing.B) {
		benchmarkVimls(b, corpus.legacy, corpus.legacyBytes, &looseWarmupOnce)
	})
	for index, workers := range []int{1, 2, 4} {
		b.Run(fmt.Sprintf("vimls-go-batch-workers-%d", workers), func(b *testing.B) {
			benchmarkVimlsBatch(b, corpus.legacy, corpus.legacyBytes, workers, &batchWarmupOnce[index])
		})
		b.Run(fmt.Sprintf("vimls-go-all-workers-%d", workers), func(b *testing.B) {
			benchmarkVimlsBatch(b, corpus.all, corpus.totalBytes, workers, &allWarmupOnce[index])
		})
	}
}

func benchmarkVimls(b *testing.B, files []inputFile, totalBytes int64, warmupOnce *sync.Once) {
	warmupOnce.Do(func() {
		for range benchmarkWarmups {
			vimlsSink = parseWithVimls(files)
		}
	})
	vimlsSink = nil
	goVimlparserSink = nil
	runtime.GC()
	parsed := make([]*syntax.File, len(files))
	b.ReportAllocs()
	b.SetBytes(totalBytes)
	b.ResetTimer()
	for range b.N {
		for index := range files {
			parsed[index] = (syntax.LegacyParser{}).Parse(files[index].source)
		}
	}
	b.StopTimer()
	vimlsSink = parsed
	runtime.KeepAlive(parsed)
}

func benchmarkOldParser(b *testing.B, files []inputFile, totalBytes int64) {
	oldWarmupOnce.Do(func() {
		for range benchmarkWarmups {
			goVimlparserSink = parseWithOldParser(files)
		}
	})
	vimlsSink = nil
	goVimlparserSink = nil
	runtime.GC()
	parsed := make([]oldast.Node, len(files))
	b.ReportAllocs()
	b.SetBytes(totalBytes)
	b.ResetTimer()
	for range b.N {
		for index := range files {
			parsed[index] = oldparser.NewVimLParser(false).Parse(
				oldparser.NewStringReader(files[index].lines), "",
			)
		}
	}
	b.StopTimer()
	goVimlparserSink = parsed
	runtime.KeepAlive(parsed)
}

func benchmarkVimlsBatch(b *testing.B, files []inputFile, totalBytes int64, workers int, warmupOnce *sync.Once) {
	sources := make([]string, len(files))
	for index := range files {
		sources[index] = files[index].source
	}
	warmupOnce.Do(func() {
		for range benchmarkWarmups {
			vimlsSink = workspace.ParseSources(context.Background(), sources, workers)
		}
	})
	vimlsSink = nil
	goVimlparserSink = nil
	runtime.GC()
	b.ReportAllocs()
	b.SetBytes(totalBytes)
	b.ResetTimer()
	for range b.N {
		vimlsSink = workspace.ParseSources(context.Background(), sources, workers)
	}
	b.StopTimer()
	runtime.KeepAlive(vimlsSink)
}

func parseWithVimls(files []inputFile) []*syntax.File {
	parsed := make([]*syntax.File, len(files))
	for index := range files {
		parsed[index] = (syntax.LegacyParser{}).Parse(files[index].source)
	}
	return parsed
}

func parseWithOldParser(files []inputFile) []oldast.Node {
	parsed := make([]oldast.Node, len(files))
	for index := range files {
		parsed[index] = oldparser.NewVimLParser(false).Parse(
			oldparser.NewStringReader(files[index].lines), "",
		)
	}
	return parsed
}

func loadLegacyCorpus(roots []string, manifest string) (*legacyCorpus, error) {
	paths, err := discoverVimFiles(roots, manifest)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no .vim files found; pass -root or -manifest")
	}

	corpus := &legacyCorpus{}
	hash := sha256.New()
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		input := inputFile{path: path, source: string(content)}
		input.lines = splitLines(input.source)
		corpus.all = append(corpus.all, input)
		corpus.totalBytes += int64(len(content))
		hash.Write([]byte(path))
		hash.Write([]byte{0})
		hash.Write(content)
		hash.Write([]byte{0})

		vimlsFile := syntax.Parse(input.source)
		if vimlsFile.Dialect == syntax.Vim9 {
			corpus.vim9Files++
			continue
		}
		corpus.legacy = append(corpus.legacy, input)
		corpus.legacyBytes += int64(len(content))
		vimlsClean := len(vimlsFile.Diagnostics) == 0
		if !vimlsClean {
			corpus.vimlsErrorFiles++
			corpus.vimlsDiagnostics += len(vimlsFile.Diagnostics)
			if len(corpus.vimlsExamples) < 20 {
				diagnostic := vimlsFile.Diagnostics[0]
				corpus.vimlsExamples = append(corpus.vimlsExamples,
					fmt.Sprintf("%s:%d: %s", path, diagnostic.Span.Start, diagnostic.Message))
			}
		}
		oldNode, oldClean, oldError := parseWithOldParserRecovering(input)
		if !oldClean {
			corpus.oldErrorFiles++
			if len(corpus.oldExamples) < 5 {
				corpus.oldExamples = append(corpus.oldExamples, fmt.Sprintf("%s: %s", path, oldError))
			}
		}
		if vimlsClean && oldClean {
			corpus.common = append(corpus.common, input)
			corpus.commonBytes += int64(len(content))
			countVimlsOutput(vimlsFile, &corpus.vimlsOutput, make(map[*syntax.Expression]struct{}))
			countOldOutput(oldNode, &corpus.oldOutput)
		}
	}
	corpus.hash = hex.EncodeToString(hash.Sum(nil))
	return corpus, nil
}

func parseWithOldParserRecovering(file inputFile) (node oldast.Node, accepted bool, message string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			node = nil
			accepted = false
			message = fmt.Sprint(recovered)
		}
	}()
	node = oldparser.NewVimLParser(false).Parse(oldparser.NewStringReader(file.lines), "")
	return node, true, ""
}

func countVimlsOutput(file *syntax.File, counts *vimlsOutputCounts, seen map[*syntax.Expression]struct{}) {
	counts.tokens += len(file.Tokens)
	counts.blocks += len(file.Blocks)
	for _, token := range file.Tokens {
		if token.Kind == syntax.TokenComment {
			counts.comments++
		}
	}
	var visitExpression func(*syntax.Expression)
	visitExpression = func(expression *syntax.Expression) {
		if expression == nil {
			return
		}
		if _, exists := seen[expression]; exists {
			return
		}
		seen[expression] = struct{}{}
		counts.expressions++
		for _, child := range expression.Children {
			visitExpression(child)
		}
		if expression.LambdaBody != nil {
			countVimlsOutput(expression.LambdaBody, counts, seen)
		}
	}
	var visitCommands func([]syntax.Command)
	visitCommands = func(commands []syntax.Command) {
		for index := range commands {
			command := &commands[index]
			counts.commands++
			if command.Block >= 0 || command.Embedded != nil || command.Declaration != nil ||
				command.Function != nil || command.Aggregate != nil || command.TypeAlias != nil ||
				command.Import != nil || command.For != nil || command.Heredoc != nil || command.TextBody != nil ||
				command.Keymap != nil || command.Mapping != nil || command.Highlight != nil || command.Syntax != nil || command.Set != nil || command.Substitute != nil || len(command.Expressions) > 0 ||
				len(command.Targets) > 0 || len(command.EnumValues) > 0 {
				counts.structured++
			}
			if command.TextBody != nil {
				counts.textBodies++
				counts.textLines += len(command.TextBody.Lines)
			}
			for _, expression := range command.Expressions {
				visitExpression(expression)
			}
			for _, expression := range command.Targets {
				visitExpression(expression)
			}
			if command.Declaration != nil {
				visitExpression(command.Declaration.Target)
				visitExpression(command.Declaration.Initializer)
			}
			if command.Function != nil {
				for parameterIndex := range command.Function.Parameters {
					visitExpression(command.Function.Parameters[parameterIndex].Default)
				}
			}
			if command.For != nil {
				visitExpression(command.For.Iterable)
			}
			if command.Import != nil {
				visitExpression(command.Import.Path)
			}
			if command.Highlight != nil {
				counts.highlights++
				counts.highlightAttrs += len(command.Highlight.Attributes)
			}
			if command.Syntax != nil {
				counts.syntaxCommands++
				counts.syntaxKeywords += len(command.Syntax.Keywords)
				counts.syntaxOptions += len(command.Syntax.Options)
				counts.syntaxPatterns += len(command.Syntax.Patterns)
			}
			if command.Set != nil {
				counts.setCommands++
				counts.setOptions += len(command.Set.Options)
			}
			if command.Substitute != nil {
				counts.substitutes++
				if command.Substitute.Expression != nil {
					counts.subExprs++
				}
				visitExpression(command.Substitute.Expression)
			}
			for enumIndex := range command.EnumValues {
				visitExpression(command.EnumValues[enumIndex].Initializer)
				for _, argument := range command.EnumValues[enumIndex].Arguments {
					visitExpression(argument)
				}
			}
			if command.Embedded != nil {
				counts.blocks += len(command.Embedded.Blocks)
				visitCommands(command.Embedded.Commands)
			}
		}
	}
	visitCommands(file.Commands)
}

func countOldOutput(node oldast.Node, counts *oldOutputCounts) {
	oldast.Inspect(node, func(node oldast.Node) bool {
		counts.nodes++
		if _, ok := node.(oldast.Expr); ok {
			counts.expressions++
		}
		if _, ok := node.(oldast.Statement); ok {
			counts.statements++
			if _, opaque := node.(*oldast.Excmd); opaque {
				counts.opaque++
			} else if _, comment := node.(*oldast.Comment); comment {
				counts.comments++
			} else {
				counts.typed++
			}
		}
		return true
	})
}

func discoverVimFiles(roots []string, manifest string) ([]string, error) {
	seen := make(map[string]struct{})
	add := func(path string) error {
		if filepath.Ext(path) != ".vim" {
			return nil
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		canonical, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return err
		}
		seen[canonical] = struct{}{}
		return nil
	}

	if manifest != "" {
		content, err := os.ReadFile(manifest)
		if err != nil {
			return nil, fmt.Errorf("read manifest: %w", err)
		}
		for _, line := range strings.Split(string(content), "\n") {
			path := strings.TrimSpace(line)
			if path != "" {
				if err := add(path); err != nil {
					return nil, fmt.Errorf("manifest path %s: %w", path, err)
				}
			}
		}
	}

	for _, root := range roots {
		absolute, err := filepath.Abs(root)
		if err != nil {
			return nil, err
		}
		canonical, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return nil, fmt.Errorf("resolve root %s: %w", root, err)
		}
		info, err := os.Stat(canonical)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			if err := add(canonical); err != nil {
				return nil, err
			}
			continue
		}
		err = filepath.WalkDir(canonical, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			return add(path)
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", root, err)
		}
	}

	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func splitLines(source string) []string {
	if source == "" {
		return nil
	}
	lines := strings.Split(source, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for index := range lines {
		lines[index] = strings.TrimSuffix(lines[index], "\r")
	}
	return lines
}

func TestSplitLinesMatchesScannerShape(t *testing.T) {
	tests := []struct {
		source string
		want   []string
	}{
		{source: "", want: nil},
		{source: "one", want: []string{"one"}},
		{source: "one\n", want: []string{"one"}},
		{source: "one\r\ntwo\r\n", want: []string{"one", "two"}},
		{source: "one\n\n", want: []string{"one", ""}},
	}
	for _, test := range tests {
		got := splitLines(test.source)
		if fmt.Sprint(got) != fmt.Sprint(test.want) {
			t.Errorf("splitLines(%q) = %#v, want %#v", test.source, got, test.want)
		}
	}
}
