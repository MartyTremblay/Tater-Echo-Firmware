package platform

import "syscall"

// Kernel returns the running kernel's machine and release, as `uname -m` and
// `uname -r` print them, or empty strings if uname fails.
//
// The raw values let the controller distinguish kernel variants and are useful
// when diagnosing a board that has not completed its Linux factory reset.
func Kernel() (machine, release string) {
	var u syscall.Utsname
	if syscall.Uname(&u) != nil {
		return "", ""
	}
	return cstr(u.Machine[:]), cstr(u.Release[:])
}

// Utsname's fields are int8 on some architectures and uint8 on others.
func cstr[T ~int8 | ~uint8](b []T) string {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if c == 0 {
			break
		}
		out = append(out, byte(c))
	}
	return string(out)
}
