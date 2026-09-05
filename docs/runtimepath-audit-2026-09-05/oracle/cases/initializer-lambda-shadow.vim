vim9script
def Test()
  var cmds = ['ok']
  const cmd = join(map(cmds, (_, cmd: string) => toupper(cmd)), "\n")
  assert_equal('OK', cmd)
enddef
Test()
