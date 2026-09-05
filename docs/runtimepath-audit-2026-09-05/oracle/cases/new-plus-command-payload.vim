keepalt new +setlocal\ previewwindow|setlocal\ buftype=nofile|setlocal\ noswapfile|setlocal\ wrap [Document]
call assert_equal('nofile', &l:buftype)
call assert_equal(0, &l:swapfile)
call assert_equal(1, &l:previewwindow)
call assert_equal(1, &l:wrap)
