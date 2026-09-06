vim9script

var plain =<< END
alpha

enddef
}
END
assert_equal(['alpha', '', 'enddef', '}'], plain)

def Text(): list<string>
  var lines =<< trim END
    nested

    text
  END
  return lines
enddef
assert_equal(['nested', '', 'text'], Text())

command HeredocOracle {
  var lines =<< trim END
    payload
  END
  assert_equal(['payload'], lines)
}
HeredocOracle
