# Reference Sources

The files in `docs/reference/` are **upstream CPython documentation**, captured
as the source-of-truth superset that minipy reduces to a subset. They describe
**full Python**, not minipy. The minipy language is defined in `docs/spec/`.

| File | Upstream URL | Retrieved | Python version |
|---|---|---|---|
| `python-grammar.md` | <https://docs.python.org/3.13/reference/grammar.html> | 2026-06-23 | 3.13 (PEG; identical to 3.14 for the subset) |
| `python-lexical.md` | <https://docs.python.org/3.13/reference/lexical_analysis.html> | 2026-06-23 | 3.13 |
| `python-datamodel.md` | <https://docs.python.org/3.13/reference/datamodel.html> | 2026-06-23 | 3.13 (summarized, link-only for full text) |

Additional upstream chapters referenced but not copied in full (read online):

- Simple statements — <https://docs.python.org/3.13/reference/simple_stmts.html>
- Compound statements — <https://docs.python.org/3.13/reference/compound_stmts.html>
- Expressions — <https://docs.python.org/3.13/reference/expressions.html>
- Execution model — <https://docs.python.org/3.13/reference/executionmodel.html>

## License

Python documentation is © Python Software Foundation, licensed under the
[PSF License Agreement](https://docs.python.org/3/license.html). These captures
are included for reference under that license. minipy's own specification
(`docs/spec/`) and code are licensed per the repository `LICENSE`.

## Baseline

minipy pins **CPython 3.13** as its reference baseline. Any minipy construct must
be valid Python 3.13 with identical surface syntax; minipy only *removes* and
*constrains*, never adds new syntax that 3.13 would reject.
