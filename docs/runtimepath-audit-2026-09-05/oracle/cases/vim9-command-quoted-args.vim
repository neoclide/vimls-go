vim9script
def Qf_filter(qf: bool, searchpat: string, bang: string)
  assert_equal('needle', searchpat)
  assert_equal('!', bang)
enddef
command! -nargs=+ -bang Cfilter Qf_filter(true, <q-args>, <q-bang>)
command! -nargs=+ -bang Lfilter Qf_filter(false, <q-args>, <q-bang>)
Cfilter! needle
Lfilter! needle
