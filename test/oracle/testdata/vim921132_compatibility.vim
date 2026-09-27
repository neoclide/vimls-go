vim9script

# Vim v9.2.1132, f3dc0fee778439ac8ff8680b42552f7f13d47396.
# Sources: src/testdir/test_vim9_script.vim Test_white_space_after_command(),
# test_vim9_assign.vim Test_assign_string_only(), and test_source.vim
# Test_source_dryrun(). Skipped bodies come from test_usercommands.vim
# Test_command_block_skipped() and test_vim9_class.vim Test_class_body_skipped().
# Builtin return types come from src/evalfunc.c ret_number(), ret_bool(), and
# ret_list_number().
# They cover the patch 1094 command whitespace, 1104 return helpers, 1130
# string-only assignments, and 1132 skipped-body behaviors.

def SourceFixture(lines: list<string>, dryrun = false): string
  var path = tempname()
  writefile(lines, path)
  var exception = ''
  try
    execute 'source' .. (dryrun ? ' ++dryrun ' : ' ') .. fnameescape(path)
  catch
    exception = v:exception
  finally
    delete(path)
  endtry
  return exception
enddef

for command in ['ch_log', 'ch_log "message"', 'echo_x', 'Foo_bar', 'exit_cb: Func})']
  assert_match('E492:', SourceFixture(['vim9script', command]), 'underscore-script-' .. command)
  assert_match('E476:', SourceFixture(['vim9script', 'def Check()', command, 'enddef', 'defcompile']), 'underscore-def-' .. command)
endfor

for target in ['$VIMLS_STRING_ONLY', '@a', 'v:errmsg']
  for assignment in [' = 123', ' ..= 123', ' = true']
    assert_match('E1012:', SourceFixture(['vim9script', target .. assignment]), 'string-only-script-' .. target .. assignment)
    assert_match('E1012:', SourceFixture(['vim9script', 'def Check()', target .. assignment, 'enddef', 'defcompile']), 'string-only-def-' .. target .. assignment)
  endfor
endfor
assert_equal('', SourceFixture(['vim9script', '$VIMLS_STRING_ONLY = "text"', '@a ..= "text"', 'v:errmsg ..= "text"', '@# = bufnr()']), 'string-only-valid')
assert_equal('', SourceFixture(['let $VIMLS_STRING_ONLY = 123', 'let @a = 456', 'let v:errmsg = 789']), 'string-only-legacy')
assert_equal('123', $VIMLS_STRING_ONLY)
assert_equal('456', @a)
assert_equal('789', v:errmsg)
unlet $VIMLS_STRING_ONLY

assert_equal('', SourceFixture([
      'vim9script',
      'def Positive()',
      '  var position: number = match("text", "x")',
      '  var added: bool = autocmd_add([])',
      '  var elapsed: list<number> = reltime()',
      'enddef',
      'defcompile',
      ]), 'builtin-return-types')
for source in [
      ['var value: string = match("text", "x")'],
      ['var value: number = autocmd_add([])'],
      ['var value: list<string> = reltime()'],
      ]
  assert_match('E1012:', SourceFixture(['vim9script', 'def Negative()'] + source + ['enddef', 'defcompile']), 'builtin-return-type-' .. source[0])
endfor

unlet! g:vimls_dryrun_side_effect
assert_equal('', SourceFixture([
      'vim9script',
      'g:vimls_dryrun_side_effect = true',
      'def Good(): number',
      '  return 1',
      'enddef',
      ], true), 'dryrun-good')
assert_false(exists('g:vimls_dryrun_side_effect'))
assert_match('E1012:', SourceFixture([
      'vim9script',
      'g:vimls_dryrun_side_effect = true',
      'def Broken(): number',
      '  return "wrong"',
      'enddef',
      ], true), 'dryrun-broken')
assert_false(exists('g:vimls_dryrun_side_effect'))

unlet! g:vimls_skipped_blocks
assert_equal('', SourceFixture([
      'vim9script',
      'if 0',
      '  command! -bar -nargs=? Xfoo {',
      '    echo <q-args>',
      '  }',
      'endif',
      'if 0',
      '  autocmd BufRead *.xyz {',
      '    echo <q-args>',
      '  }',
      'endif',
      'while 0',
      '  interface SkipInterface',
      '    def M(): number',
      '  endinterface',
      '  class SkipClass',
      '    var value: number = 1',
      '  endclass',
      '  enum SkipEnum',
      '    One',
      '  endenum',
      'endwhile',
      'g:vimls_skipped_blocks = "ran"',
      ]), 'skipped-blocks')
assert_equal('ran', g:vimls_skipped_blocks)
unlet g:vimls_skipped_blocks
