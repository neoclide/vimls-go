set nomore
set encoding=utf-8
let &runtimepath = '/Users/chemzqm/vim-dev/vimls-go/docs/runtimepath-audit-2026-09-05/oracle/runtime'
let g:oracle_exception = ''
let g:oracle_throwpoint = ''
try
  execute 'source ' . fnameescape('/Users/chemzqm/vim-dev/vimls-go/docs/runtimepath-audit-2026-09-05/oracle/cases/session-listchars-47.vim')
catch
  let g:oracle_exception = v:exception
  let g:oracle_throwpoint = v:throwpoint
endtry
call writefile([json_encode({'version': v:version, 'patch1015': has('patch-9.2.1015'), 'patch1016': has('patch-9.2.1016'), 'errors': v:errors, 'messages': execute('messages'), 'exception': g:oracle_exception, 'throwpoint': g:oracle_throwpoint})], '/Users/chemzqm/vim-dev/vimls-go/docs/runtimepath-audit-2026-09-05/oracle/session-listchars-47.json')
if empty(g:oracle_exception) && empty(v:errors)
  qa!
else
  cquit 1
endif
