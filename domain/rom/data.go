package rom

type ROMWithFile struct {
	ROM    ROM
	SHA256 string
	// DiscCount is how many discs the ROM has.
	DiscCount int
}
