//go:build linux

package cve_test

import "syscall"

// syscallTerm is the signal that asks a program to end.
const syscallTerm = syscall.SIGTERM
