vim9script

# Curated differences from v9.2.1132 through v9.2.1235,
# commit 12d91ed7bb9c7276216670aed8e4fee264a13703.
# Sources: test_vim9_script.vim try termination (patch 1155),
# test_vim9_assign.vim environment declarations (patch 1232), and
# test_vim9_generics.vim interface method type-variable counts (patch 1209).
def Probe(lines: list<string>): string
  var path = tempname()
  writefile(lines, path)
  try
    execute 'source ' .. fnameescape(path)
  catch
    return v:exception
  finally
    delete(path)
  endtry
  return ''
enddef

for body in [
      ['try', 'return "ok"', 'catch /.*/', 'return "caught"', 'endtry'],
      ['try', 'throw "error"', 'catch /error/', 'return "caught"', 'endtry'],
      ['try', 'return "ok"', 'catch', 'throw "error"', 'endtry'],
      ['try', 'return "ok"', 'finally', 'echo "done"', 'endtry'],
      ['try', 'echo "work"', 'finally', 'throw "error"', 'endtry'],
      ]
  var head = ['vim9script', 'def Check(): string'] + body
  assert_equal('', Probe(head + ['enddef', 'defcompile']), string(body))
  assert_match('E1095:', Probe(head + ['return "unreachable"', 'enddef', 'defcompile']), string(body))
endfor
assert_match('E1027:', Probe(['vim9script', 'def Check(): string', 'try', 'echo "work"', 'catch', 'return "caught"', 'endtry', 'enddef', 'defcompile']))
for jump in ['break', 'continue']
  assert_match('E1095:', Probe(['vim9script', 'def Check()', 'for n in [1]', jump, 'echo n', 'endfor', 'enddef', 'defcompile']))
endfor
assert_match('E1000:', Probe(['vim9script', 'var $VIMLS_DECLARED_ENV = 1']))
assert_match('E1000:', Probe(['vim9script', 'def Check()', 'var $VIMLS_DECLARED_ENV: number', 'enddef', 'defcompile']))
assert_match('E484:', Probe(['source ++dryrun vimls_missing_fixture.vim']))

assert_match('E1438:', Probe([
      'vim9script',
      'interface I',
      '  def Fn<T>(t: T): T',
      'endinterface',
      'class A implements I',
      '  def Fn(t: number): number',
      '    return t',
      '  enddef',
      'endclass',
      ]))
assert_equal('', Probe([
      'vim9script',
      'interface I',
      '  def Copy(): I',
      'endinterface',
      'class A implements I',
      '  def Copy(): A',
      '    return A.new()',
      '  enddef',
      'endclass',
      ]))
assert_equal(2, false ? <nosuch>1 : 2)
assert_equal(2, true ? 2 : <nosuch>1)
assert_equal('', Probe(['vim9script', 'def Check(): number', 'return false ? <nosuch>1 : 2', 'enddef', 'defcompile']))
assert_equal('len', getinfo('function', 'len').name)
assert_true(has_key(getinfo('vimvar', 'v:imagebackend'), 'type'))
assert_equal('', Probe([
      'vim9script',
      'def Check()',
      '  v:clipproviders["vimls_probe"] = {}',
      '  remove(v:clipproviders, "vimls_probe")',
      'enddef',
      'Check()',
      ]))
