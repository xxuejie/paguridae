# paguridae

A Web IDE hugely inspired from [Acme](http://acme.cat-v.org/) that is almost a clone of acme :)

# Build Dependencies

* [esc](https://github.com/mjibson/esc)
* [esbuild](https://github.com/evanw/esbuild)

# Internals

## Hard Requirements

* Initial page load must have only 1 request, gzipped transfer size must be less than 100KB.
* The compiled binary must be a staticically linked one with no dependencies.
* The binary must run on Linux OSes using x86-64, ARM/ARM64 and MIPS CPUs. Production binary should be less than 20MB.

## Decisions

* By default long running programs are opened the same way as acme(enter sends commands after insert point to program), a command can change to raw mode, where each key press gets send to the program immediately(questionable if we can do RAW mode detection, need to check tty in more details)

## Noticeable future deps

* [chroma](https://github.com/alecthomas/chroma) for syntax highlighting, syntax highlighting will be a separate tool. The core will only provide a special 9P file, the file accepts Delta format with styles attached to buffers.
* [pty](https://github.com/creack/pty) in a new win command for RAW mode support. The core only provides a flag that sends each pressed character immediately to the backend.
