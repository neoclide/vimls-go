# Option assignment errors

These diagnostics follow Vim v9.2.1015 for direct option assignments, except
for the simplified boolean compound-assignment policy below.
Known options are assumed to exist regardless of build-feature conditions.
Unknown RHS types and Vim9 `any` skip type checks; value-dependent errors require
statically known values.

| Code | Meaning / example |
| --- | --- |
| E474 | Invalid option-setting syntax, e.g. `set hidden=abc`. |
| E521 | Number required, e.g. `set history=abc` or `let &hidden = 'abc'`; also invalid boolean option compound assignment in a Vim9 `def`. |
| E928 | String required, e.g. `let &titlestring = v:true`. |
| E734 | Assignment operator incompatible with the option, e.g. `let &titlestring += 1`. |
| E703 / E729 | Using a Funcref as a Number / String. |
| E745 / E730 | Using a List as a Number / String. |
| E728 / E731 | Using a Dictionary as a Number / String. |
| E974 / E976 | Using a Blob as a Number / String. |
| E805 | Using a Float as a Number. |
| E611 | Using a Special as a Number. |
| E1138 | Using a Bool as a Number. |
| E1023 | Using a Number other than 0 or 1 as a Bool. |
| E1012 | Type mismatch in a Vim9 compiled assignment. |
| E1019 | Concatenating to a non-string option in Vim9 compiled code. |
| E1051 / E1035 / E1036 | Invalid operands for compiled arithmetic assignment. |

The error code depends on Legacy, Vim9 script or `def` context. As a deliberate
simplification, invalid boolean option compound assignments in a Vim9 `def`
uniformly report E521 instead of Vim's RHS-dependent type and operator codes.
Unknown and `any` RHS types still skip this check.
