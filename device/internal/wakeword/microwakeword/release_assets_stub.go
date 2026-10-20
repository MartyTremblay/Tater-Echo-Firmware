package microwakeword

// Tater Linux root filesystems carry these native companions as ordinary,
// independently verified files. The daemon therefore embeds no platform
// runtime inside its executable.
var (
	embeddedReleaseRuntime        []byte
	embeddedReleaseORTRuntime     []byte
	embeddedReleaseMelspectrogram []byte
	embeddedReleaseEmbedding      []byte
)
