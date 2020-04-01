# paguridae

A Web IDE hugely inspired from [Acme](http://acme.cat-v.org/) that is almost a clone of acme :)

# Build Dependencies

* [esc](https://github.com/mjibson/esc)
* [esbuild](https://github.com/evanw/esbuild)

# Internals

## Decisions

* By default long running programs are opened the same way as acme(enter sends commands after insert point to program), a command can change to raw mode, where each key press gets send to the program immediately(questionable if we can do RAW mode detection, need to check tty in more details)

## Future deps

* [chroma](https://github.com/alecthomas/chroma) for syntax highlighting
* Need to write a small library that supports slicing a large file into a small delta section with custom embed for holes at each end

* [pty](https://github.com/creack/pty) in a new win command for RAW mode support
