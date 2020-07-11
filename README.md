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
* The total number of lines in this repository, including Go, JavaScript and CSS code, should be less than 10000.
