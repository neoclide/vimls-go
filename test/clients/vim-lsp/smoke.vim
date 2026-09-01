let v:errors = []
let s:legacy_counts = {}
let s:vim9_counts = {}
let s:running_status = ''
let s:stopped_status = ''
let s:shutdown_done = 0

function! s:has_error_diagnostic() abort
  return get(lsp#get_buffer_diagnostics_counts(), 'error', 0) > 0
endfunction

function! s:shutdown_response(response) abort
  let s:shutdown_done = 1
endfunction

try
  if v:version != 902 || !has('patch-9.2.1015') || has('patch-9.2.1016')
    call add(v:errors, 'expected exact Vim patch v9.2.1015')
  endif
  execute 'edit ' .. fnameescape($VIMLS_CLIENT_WORKSPACE .. '/legacy.vim')
  setfiletype vim
  call assert_equal('vim', &filetype)
  if lsp#utils#_wait(10000, {-> lsp#get_server_status('vimls') ==# 'running'}, 10) != 0
    call add(v:errors, 'vimls did not initialize: ' .. lsp#get_server_status('vimls'))
  endif
  let s:running_status = lsp#get_server_status('vimls')
  if lsp#utils#_wait(10000, function('s:has_error_diagnostic'), 10) != 0
    call add(v:errors, 'legacy diagnostics timed out')
  endif
  let s:legacy_counts = lsp#get_buffer_diagnostics_counts()

  execute 'edit ' .. fnameescape($VIMLS_CLIENT_WORKSPACE .. '/vim9.vim')
  setfiletype vim
  call assert_equal('vim', &filetype)
  if lsp#utils#_wait(10000, function('s:has_error_diagnostic'), 10) != 0
    call add(v:errors, 'Vim9 diagnostics timed out')
  endif
  let s:vim9_counts = lsp#get_buffer_diagnostics_counts()

  call lsp#send_request('vimls', {
        \ 'method': 'shutdown',
        \ 'params': v:null,
        \ 'on_notification': function('s:shutdown_response'),
        \ })
  if lsp#utils#_wait(5000, {-> s:shutdown_done}, 10) != 0
    call add(v:errors, 'shutdown response timed out')
  else
    call lsp#callbag#pipe(
          \ lsp#notification('vimls', {'method': 'exit', 'params': v:null}),
          \ lsp#callbag#subscribe())
    if lsp#utils#_wait(5000, {-> lsp#get_server_status('vimls') ==# 'exited'}, 10) != 0
      call add(v:errors, 'vimls did not exit: ' .. lsp#get_server_status('vimls'))
    endif
  endif
  let s:stopped_status = lsp#get_server_status('vimls')
catch
  call add(v:errors, v:exception .. ' @ ' .. v:throwpoint)
endtry

call writefile([
      \ 'version=' .. execute('version')->split("\n")[0],
      \ 'patch-9.2.1015=' .. has('patch-9.2.1015'),
      \ 'patch-9.2.1016=' .. has('patch-9.2.1016'),
      \ 'running_status=' .. s:running_status,
      \ 'legacy_diagnostics=' .. string(s:legacy_counts),
      \ 'vim9_diagnostics=' .. string(s:vim9_counts),
      \ 'shutdown_response=' .. s:shutdown_done,
      \ 'stopped_status=' .. s:stopped_status,
      \ 'v:errors=' .. string(v:errors),
      \ 'messages=' .. substitute(execute('messages'), "\n", '\\n', 'g'),
      \ ], $VIMLS_CLIENT_RESULT)
if !empty(v:errors)
  cquit 1
endif
qa!
