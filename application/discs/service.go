package discs

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/flate"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/rom"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

// Repository stores ROM files and their discs.
type Repository interface {
	FindROMFileByID(context.Context, string) (*romdomain.ROMFile, error)
	FindROMFileBySHA256(context.Context, string) (*romdomain.ROMFile, error)
	FindROMFileByDiscIdentity(context.Context, string) (*romdomain.ROMFile, error)
	UpsertROMFile(context.Context, romdomain.ROMFile) (romdomain.ROMFile, error)
	FindDiscTracks(context.Context, string) ([]romdomain.DiscTrack, error)
	SaveDisc(ctx context.Context, romFileID string, format romdomain.FileFormat, identity *string, tracks []romdomain.DiscTrack) error
	ListROMFilesByFormat(ctx context.Context, format romdomain.FileFormat, limit int) ([]romdomain.ROMFile, error)
	ListCompressionRequests(ctx context.Context, limit int) ([]romdomain.ROMFile, error)
	SetCompressRequested(ctx context.Context, romFileID string, requested bool) error
	ReplaceROMFileContent(ctx context.Context, romFileID string, hash romdomain.ROMHash, size uint64, storagePath string, format romdomain.FileFormat) error
}

// Storage holds stored files, addressed by content.
type Storage interface {
	Write(ctx context.Context, contentAddress string, content []byte) (string, error)
	Read(ctx context.Context, storagePath string) ([]byte, error)
	Open(ctx context.Context, storagePath string) (rom.StoredContent, error)
	LocalPath(storagePath string) (string, error)
	WorkDir() (string, error)
	Adopt(ctx context.Context, path, contentAddress string) (string, error)
}

// readerAt returns random access to opened stored content, which local storage offers.
func readerAt(content rom.StoredContent) (io.ReaderAt, error) {
	if reader, ok := content.ReadSeekCloser.(io.ReaderAt); ok {
		return reader, nil
	}
	return nil, errors.New("discs: stored content does not support random access")
}

// Hasher computes the hash bundle that identifies content.
type Hasher interface {
	NewDigest() romdomain.Digest
}

// Images reads CHD images.
type Images interface {
	// Inspect returns the tracks of the CHD in content, hashed as their track files, naming them
	// after base; ok is false when the content is not a CD or GD-ROM CHD.
	Inspect(ctx context.Context, content io.ReaderAt, size int64, base string) (tracks []romdomain.DiscTrack, ok bool, err error)
	// Extract writes the CHD's sheet and track files, named after base, through add.
	Extract(ctx context.Context, content io.ReaderAt, size int64, base string, add AddFile) error
	// WriteTrack writes the file of track number of the CHD in content.
	WriteTrack(ctx context.Context, content io.ReaderAt, size int64, number int, out io.Writer) error
}

// AddFile adds a file of size bytes, written by write, to an archive.
type AddFile func(name string, size int64, write func(io.Writer) error) error

// Compressor makes CHD images. It is optional: a service without one does not compress.
type Compressor interface {
	// Compress makes a CHD at output from the CUE sheet at sheetPath and its track files beside
	// it, telling progress how many bytes of the disc are done.
	Compress(ctx context.Context, sheetPath, output string, progress func(done, total uint64)) error
}

// Service indexes, assembles, compresses, and archives discs.
type Service struct {
	repository Repository
	storage    Storage
	hasher     Hasher
	images     Images
	compressor Compressor

	// Discs compress one at a time, so these describe the one under way, if any.
	progressLock    sync.RWMutex
	progressFileID  string
	progressPercent int
}

func NewService(repository Repository, storage Storage, hasher Hasher, images Images, compressor Compressor) *Service {
	return &Service{repository: repository, storage: storage, hasher: hasher, images: images, compressor: compressor}
}

// CanCompress reports whether discs are compressed to CHD.
func (service *Service) CanCompress() bool { return service.compressor != nil }

// CompressionProgress reports how far along a disc being compressed is, and false for any other
// disc. 100 means compressing is done and the result is being checked; compressing itself reports
// at most 99, so a full bar is never left unexplained. It is kept in memory: after a restart a
// disc starts over, and only the request survives.
func (service *Service) CompressionProgress(romFileID string) (int, bool) {
	service.progressLock.RLock()
	defer service.progressLock.RUnlock()
	if romFileID == "" || service.progressFileID != romFileID {
		return 0, false
	}
	return service.progressPercent, true
}

func (service *Service) setCompressionProgress(romFileID string, percent int) {
	service.progressLock.Lock()
	defer service.progressLock.Unlock()
	service.progressFileID, service.progressPercent = romFileID, percent
}

// compressedPercent is how far along compressing done of total bytes is, short of the check.
func compressedPercent(done, total uint64) int {
	if total == 0 {
		return 0
	}
	return int(min(99, done*100/total))
}

// CanCompressFile reports whether this disc can be compressed: it is kept as tracks and is not a
// GD-ROM, which stays as uploaded since the compressor does not make GD-ROM CHDs. A sheet that
// cannot be read means no, so that a ROM's page does not fail over it.
func (service *Service) CanCompressFile(ctx context.Context, romFile romdomain.ROMFile) bool {
	if service.compressor == nil || romFile.Format != romdomain.FormatTracks {
		return false
	}
	sheet, err := service.storage.Read(ctx, sheetPath(romFile))
	if err != nil {
		slog.WarnContext(ctx, "could not read a disc's sheet", "rom_file_id", romFile.ID, "error", err)
		return false
	}
	return compressibleSheet(sheet)
}

// Index inspects a newly stored (or not yet inspected) file: a CD or GD-ROM CHD gets its tracks
// and disc identity recorded, so its tracks' hashes can identify it; anything else is marked a
// plain file. A disc the library already holds, in any form, is reported as a duplicate.
func (service *Service) Index(ctx context.Context, romFile romdomain.ROMFile, base string) error {
	if romFile.Format != romdomain.FormatFile && romFile.Format != romdomain.FormatPending {
		return nil
	}
	content, err := service.storage.Open(ctx, romFile.StoragePath)
	if err != nil {
		return err
	}
	defer content.Close()
	random, err := readerAt(content)
	if err != nil {
		return err
	}
	tracks, ok, err := service.images.Inspect(ctx, random, content.Size, base)
	if err != nil {
		// A damaged or unsupported CHD stays a plain file: it can still be downloaded.
		slog.WarnContext(ctx, "could not read disc image", "rom_file_id", romFile.ID, "error", err)
		ok = false
	}
	if !ok {
		return service.repository.SaveDisc(ctx, romFile.ID, romdomain.FormatFile, nil, nil)
	}
	identity := romdomain.DiscIdentity(tracks)
	if existing, err := service.repository.FindROMFileByDiscIdentity(ctx, identity); err != nil {
		return err
	} else if existing != nil && existing.ID != romFile.ID {
		return fault.ROMAlreadyExists(existing.ID, identity)
	}
	if err := service.repository.SaveDisc(ctx, romFile.ID, romdomain.FormatCHD, &identity, tracks); err != nil {
		if fault.IsCode(err, fault.CodeConflict) {
			existing, _ := service.repository.FindROMFileByDiscIdentity(ctx, identity)
			if existing != nil {
				return fault.ROMAlreadyExists(existing.ID, identity)
			}
		}
		return err
	}
	return nil
}

// Part is one uploaded file of a disc.
type Part struct {
	Name        string
	StoragePath string
	Size        int64
	Hash        romdomain.ROMHash
}

// Assemble records a disc uploaded as its sheet and track files, and asks for it to be
// compressed when compress is set. Every track is stored as a file of its own (a file holding
// several tracks is split), each remembering the file it came from, and the sheet is kept as
// uploaded: downloading the disc gives back exactly the files that were uploaded. The disc is
// checked against the library by its identity, so a disc stored before as a CHD is found too.
func (service *Service) Assemble(ctx context.Context, sheetName, sheetText string, parts []Part, compress bool) (romdomain.ROMFile, error) {
	if compress && service.compressor == nil {
		return romdomain.ROMFile{}, ErrNoCompressor
	}
	kind, ok := SheetKindFor(sheetName)
	if !ok {
		return romdomain.ROMFile{}, fault.Validation("a disc's sheet must be a .cue or .gdi file")
	}
	// A GD-ROM is kept as uploaded rather than refused for having asked for compression.
	compress = compress && compressibleSheet([]byte(sheetText))
	sheet, err := ParseSheet(kind, sheetText)
	if err != nil {
		return romdomain.ROMFile{}, err
	}
	byName := make(map[string]Part, len(parts))
	sizes := make(map[string]int64, len(parts))
	for _, part := range parts {
		byName[part.Name] = part
		sizes[part.Name] = part.Size
	}
	for _, name := range sheet.Files() {
		if _, ok := byName[name]; !ok {
			return romdomain.ROMFile{}, fault.Validation(fmt.Sprintf("the sheet needs %q, which was not uploaded", name))
		}
	}
	if len(parts) != len(sheet.Files()) {
		return romdomain.ROMFile{}, fault.Validation("every uploaded file must be named in the sheet")
	}
	spans, err := sheet.Layout(sizes)
	if err != nil {
		return romdomain.ROMFile{}, err
	}
	tracks := make([]romdomain.DiscTrack, len(spans))
	for index, span := range spans {
		part := byName[span.Track.File]
		track := romdomain.DiscTrack{
			Number: span.Track.Number, Mode: span.Track.Mode, SectorBytes: span.Track.SectorBytes,
			Frames: int(span.Length / int64(span.Track.SectorBytes)), Pregap: span.Track.Pregap,
			PregapStored: span.Track.PregapStored, Postgap: span.Track.Postgap, Size: span.Length,
			Filename: span.Track.File,
		}
		storagePath := part.StoragePath
		if span.Offset == 0 && span.Length == part.Size {
			track.Hash = part.Hash
		} else if track.Hash, storagePath, err = service.splitTrack(ctx, part.StoragePath, span.Offset, span.Length); err != nil {
			// The track is part of a larger file: it is stored alone, and joined back on download.
			return romdomain.ROMFile{}, err
		}
		track.StoragePath = &storagePath
		tracks[index] = track
	}
	identity := romdomain.DiscIdentity(tracks)
	if existing, err := service.repository.FindROMFileByDiscIdentity(ctx, identity); err != nil {
		return romdomain.ROMFile{}, err
	} else if existing != nil {
		return romdomain.ROMFile{}, fault.ROMAlreadyExists(existing.ID, identity)
	}
	sheetSum := sha256.Sum256([]byte(sheetText))
	sheetPath, err := service.storage.Write(ctx, hex.EncodeToString(sheetSum[:]), []byte(sheetText))
	if err != nil {
		return romdomain.ROMFile{}, err
	}
	// A disc kept as tracks is identified by its identity; its hashes are those of its manifest.
	manifest := service.hasher.NewDigest()
	for _, track := range tracks {
		fmt.Fprintf(manifest, "%d %s %d %s\n", track.Number, track.Mode, track.Size, track.Hash.SHA1)
	}
	hash, err := manifest.Sum()
	if err != nil {
		return romdomain.ROMFile{}, fault.Internal(err)
	}
	hash.SHA256 = identity
	var total int64
	for _, track := range tracks {
		total += track.Size
	}
	romFile, err := service.repository.UpsertROMFile(ctx, romdomain.ROMFile{
		ID: "rf_" + identity, Hash: hash, Size: uint64(total), StoragePath: sheetPath, Format: romdomain.FormatTracks,
		SheetPath: &sheetPath, CompressRequested: compress,
	})
	if err != nil {
		return romdomain.ROMFile{}, err
	}
	if err := service.repository.SaveDisc(ctx, romFile.ID, romdomain.FormatTracks, &identity, tracks); err != nil {
		return romdomain.ROMFile{}, err
	}
	romFile.Format, romFile.DiscIdentity = romdomain.FormatTracks, &identity
	return romFile, nil
}

// splitTrack stores bytes [offset, offset+length) of a stored file as a file of its own.
func (service *Service) splitTrack(ctx context.Context, storagePath string, offset, length int64) (romdomain.ROMHash, string, error) {
	source, err := service.storage.Open(ctx, storagePath)
	if err != nil {
		return romdomain.ROMHash{}, "", err
	}
	defer source.Close()
	work, err := service.storage.WorkDir()
	if err != nil {
		return romdomain.ROMHash{}, "", err
	}
	defer os.RemoveAll(work)
	path := filepath.Join(work, "track")
	file, err := os.Create(path)
	if err != nil {
		return romdomain.ROMHash{}, "", err
	}
	digest := service.hasher.NewDigest()
	random, err := readerAt(source)
	if err != nil {
		file.Close()
		return romdomain.ROMHash{}, "", err
	}
	_, copyErr := io.Copy(io.MultiWriter(file, digest), io.NewSectionReader(random, offset, length))
	if err := errors.Join(copyErr, file.Close()); err != nil {
		return romdomain.ROMHash{}, "", err
	}
	hash, err := digest.Sum()
	if err != nil {
		return romdomain.ROMHash{}, "", fault.Internal(err)
	}
	key, err := service.storage.Adopt(ctx, path, hash.SHA256)
	return hash, key, err
}

// splitTrackName names a split track file as Redump (and chdman) do.
func splitTrackName(base string, tracks, number int) string {
	if tracks >= 10 {
		return fmt.Sprintf("%s (Track %02d).bin", base, number)
	}
	return fmt.Sprintf("%s (Track %d).bin", base, number)
}

// cueFor writes a CUE sheet with one file per track.
func cueFor(tracks []romdomain.DiscTrack) string {
	var sheet strings.Builder
	for _, track := range tracks {
		fmt.Fprintf(&sheet, "FILE \"%s\" BINARY\n  TRACK %02d %s\n", track.Filename, track.Number, track.Mode)
		switch {
		case track.Pregap > 0 && !track.PregapStored:
			fmt.Fprintf(&sheet, "    PREGAP %s\n    INDEX 01 00:00:00\n", msf(track.Pregap))
		case track.Pregap > 0:
			fmt.Fprintf(&sheet, "    INDEX 00 00:00:00\n    INDEX 01 %s\n", msf(track.Pregap))
		default:
			sheet.WriteString("    INDEX 01 00:00:00\n")
		}
		if track.Postgap > 0 {
			fmt.Fprintf(&sheet, "    POSTGAP %s\n", msf(track.Postgap))
		}
	}
	return sheet.String()
}

func msf(frames int) string {
	return fmt.Sprintf("%02d:%02d:%02d", frames/4500, frames/75%60, frames%75)
}

// ErrNotCompressible reports a file that is not a disc kept as tracks.
var ErrNotCompressible = errors.New("discs: not a disc kept as separate tracks")

// Compress turns a disc kept as tracks into a CHD. The CHD replaces the tracks only
// once reading it back gives every track's exact hashes; the tracks' files are then left for
// the storage sweep to remove.
func (service *Service) Compress(ctx context.Context, romFileID string) error {
	if service.compressor == nil {
		return errors.New("discs: this server cannot compress discs")
	}
	romFile, err := service.repository.FindROMFileByID(ctx, romFileID)
	if err != nil {
		return err
	}
	if romFile == nil || romFile.Format != romdomain.FormatTracks {
		return ErrNotCompressible
	}
	tracks, err := service.repository.FindDiscTracks(ctx, romFile.ID)
	if err != nil {
		return err
	}
	sheet, err := service.storage.Read(ctx, sheetPath(*romFile))
	if err != nil {
		return err
	}
	if !compressibleSheet(sheet) {
		return ErrNotCompressible
	}
	service.setCompressionProgress(romFile.ID, 0)
	defer service.setCompressionProgress("", 0)
	work, err := service.storage.WorkDir()
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	// The compressor reads one file per track, so the sheet is rewritten for the stored track
	// files (the uploaded sheet may name one file for several tracks).
	workTracks := make([]romdomain.DiscTrack, len(tracks))
	for index, track := range tracks {
		if track.StoragePath == nil {
			return fmt.Errorf("discs: track %d has no stored file", track.Number)
		}
		source, err := service.storage.LocalPath(*track.StoragePath)
		if err != nil {
			return err
		}
		track.Filename = splitTrackName("disc", len(tracks), track.Number)
		workTracks[index] = track
		// A hard link costs nothing; the compressor only reads the tracks.
		if err := os.Link(source, filepath.Join(work, track.Filename)); err != nil {
			if err := copyFile(source, filepath.Join(work, track.Filename)); err != nil {
				return err
			}
		}
	}
	sheetFile := filepath.Join(work, "disc.cue")
	if err := os.WriteFile(sheetFile, []byte(cueFor(workTracks)), 0o600); err != nil {
		return err
	}
	output := filepath.Join(work, "disc.chd")
	report := func(done, total uint64) {
		service.setCompressionProgress(romFile.ID, compressedPercent(done, total))
	}
	if err := service.compressor.Compress(ctx, sheetFile, output, report); err != nil {
		return err
	}
	service.setCompressionProgress(romFile.ID, 100)

	chd, err := os.Open(output)
	if err != nil {
		return err
	}
	defer chd.Close()
	info, err := chd.Stat()
	if err != nil {
		return err
	}
	readBack, ok, err := service.images.Inspect(ctx, chd, info.Size(), "disc")
	if err != nil || !ok {
		return fmt.Errorf("discs: the compressed CHD cannot be read back: %v", err)
	}
	if romdomain.DiscIdentity(readBack) != *romFile.DiscIdentity {
		return fmt.Errorf("discs: the compressed CHD does not reproduce the tracks exactly; keeping them")
	}
	digest := service.hasher.NewDigest()
	if _, err := io.Copy(digest, io.NewSectionReader(chd, 0, info.Size())); err != nil {
		return err
	}
	hash, err := digest.Sum()
	if err != nil {
		return fault.Internal(err)
	}
	if existing, err := service.repository.FindROMFileBySHA256(ctx, hash.SHA256); err != nil {
		return err
	} else if existing != nil {
		return fmt.Errorf("discs: the library already holds this CHD as %s", existing.ID)
	}
	key, err := service.storage.Adopt(ctx, output, hash.SHA256)
	if err != nil {
		return err
	}
	return service.repository.ReplaceROMFileContent(ctx, romFile.ID, hash, uint64(info.Size()), key, romdomain.FormatCHD)
}

// CompressPending compresses the discs asked to be compressed, one at a time, until none
// remain or ctx ends, and returns how many it compressed. A disc that fails is logged and its
// request dropped, so the disc stays as it was and can be asked again.
func (service *Service) CompressPending(ctx context.Context) (int, error) {
	if service.compressor == nil {
		return 0, nil
	}
	compressed := 0
	for ctx.Err() == nil {
		files, err := service.repository.ListCompressionRequests(ctx, 1)
		if err != nil || len(files) == 0 {
			return compressed, err
		}
		if err := service.Compress(ctx, files[0].ID); err != nil {
			if ctx.Err() != nil {
				return compressed, ctx.Err()
			}
			slog.WarnContext(ctx, "could not compress disc to CHD", "rom_file_id", files[0].ID, "error", err)
			if err := service.repository.SetCompressRequested(ctx, files[0].ID, false); err != nil {
				return compressed, err
			}
			continue
		}
		compressed++
	}
	return compressed, ctx.Err()
}

// ErrNoCompressor reports that compression was asked for on a service without a compressor.
var ErrNoCompressor = fault.Validation("this server cannot compress discs")

// RequestCompression asks for a disc kept as tracks to be compressed to CHD in the background.
func (service *Service) RequestCompression(ctx context.Context, romFile romdomain.ROMFile) error {
	if service.compressor == nil {
		return ErrNoCompressor
	}
	if !service.CanCompressFile(ctx, romFile) {
		return fault.Validation("only discs kept as CUE/BIN tracks can be compressed")
	}
	return service.repository.SetCompressRequested(ctx, romFile.ID, true)
}

// sheetPath is where a disc's uploaded sheet is kept.
func sheetPath(romFile romdomain.ROMFile) string {
	if romFile.SheetPath != nil {
		return *romFile.SheetPath
	}
	return romFile.StoragePath
}

// sheetKindOf tells a CUE sheet from a GDI file by its content.
func sheetKindOf(sheet []byte) SheetKind {
	if _, err := ParseSheet(SheetCUE, string(sheet)); err == nil {
		return SheetCUE
	}
	return SheetGDI
}

// compressibleSheet reports a CUE sheet of an ordinary CD. A GD-ROM is either a GDI file or a CUE
// sheet with REM lines naming both of its density areas, as Redump writes for Dreamcast discs;
// the sheet rewritten for compressing would lose those lines, and the CHD with them that layout.
func compressibleSheet(sheet []byte) bool {
	if sheetKindOf(sheet) != SheetCUE {
		return false
	}
	var single, high bool
	for line := range strings.Lines(strings.ToUpper(string(sheet))) {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "REM" && fields[2] == "AREA" {
			single = single || fields[1] == "SINGLE-DENSITY"
			high = high || fields[1] == "HIGH-DENSITY"
		}
	}
	return !(single && high)
}

// IndexPending inspects files stored before discs were recognized, a batch at a time.
func (service *Service) IndexPending(ctx context.Context) (int, error) {
	indexed := 0
	for ctx.Err() == nil {
		files, err := service.repository.ListROMFilesByFormat(ctx, romdomain.FormatPending, 20)
		if err != nil || len(files) == 0 {
			return indexed, err
		}
		for _, file := range files {
			err := service.Index(ctx, file, "disc")
			if fault.IsCode(err, fault.CodeROMAlreadyExists) {
				// Two files already held the same disc; keep both, indexed but without identity.
				tracks, ok := service.inspectQuietly(ctx, file)
				format := romdomain.FormatFile
				if ok {
					format = romdomain.FormatCHD
				}
				err = service.repository.SaveDisc(ctx, file.ID, format, nil, tracks)
			}
			if err != nil {
				return indexed, err
			}
			indexed++
		}
	}
	return indexed, ctx.Err()
}

func (service *Service) inspectQuietly(ctx context.Context, file romdomain.ROMFile) ([]romdomain.DiscTrack, bool) {
	content, err := service.storage.Open(ctx, file.StoragePath)
	if err != nil {
		return nil, false
	}
	defer content.Close()
	random, err := readerAt(content)
	if err != nil {
		return nil, false
	}
	tracks, ok, err := service.images.Inspect(ctx, random, content.Size, "disc")
	return tracks, ok && err == nil
}

// zipWriter writes the entries of a download zip: Deflate at the default level, stored for
// formats that are compressed already, every entry stamped with one modification time.
type zipWriter struct {
	archive  *zip.Writer
	modified time.Time
}

func newZipWriter(out io.Writer) *zipWriter {
	archive := zip.NewWriter(out)
	archive.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(w, flate.DefaultCompression)
	})
	return &zipWriter{archive: archive, modified: time.Now()}
}

// zipMethod stores the disc formats that are compressed already, and deflates the rest.
func zipMethod(name string) uint16 {
	switch strings.ToLower(path.Ext(name)) {
	case ".chd", ".cso", ".rvz", ".pbp":
		return zip.Store
	}
	return zip.Deflate
}

// safeEntryName reports whether name is a relative zip path with forward slashes and no empty,
// "." or ".." segment, so extracting it cannot leave the folder it is extracted into.
func safeEntryName(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return false
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func (writer *zipWriter) add(name string, size int64, write func(io.Writer) error) error {
	if !safeEntryName(name) {
		return fault.Validation(fmt.Sprintf("cannot put %q in a zip", name))
	}
	header := &zip.FileHeader{Name: name, Method: zipMethod(name), Modified: writer.modified}
	header.SetMode(0o644)
	if size >= 0 {
		header.UncompressedSize64 = uint64(size)
	}
	entry, err := writer.archive.CreateHeader(header)
	if err != nil {
		return err
	}
	return write(entry)
}

func (writer *zipWriter) close() error { return writer.archive.Close() }

// Archive writes a disc as a zip of its sheet and track files, named after base. A disc
// uploaded as a sheet and tracks comes back exactly as uploaded (its sheet, its file names,
// tracks that shared a file joined again), rebuilt from its CHD if it was compressed since; a
// disc uploaded as a CHD gets the sheet and files chdman extractcd would write.
func (service *Service) Archive(ctx context.Context, romFile romdomain.ROMFile, base string, out io.Writer) error {
	if romFile.Format != romdomain.FormatCHD && romFile.Format != romdomain.FormatTracks {
		return fault.Validation("this ROM is not a disc")
	}
	archive := newZipWriter(out)
	if err := service.writeDisc(ctx, romFile, base, archive.add); err != nil {
		return err
	}
	return archive.close()
}

// ArchiveDiscs writes every disc of a title in one zip, with a playlist, title.m3u, as its last
// entry. A disc kept as tracks goes in a folder named after it, since track names can repeat
// between discs; any other disc is one file, copied from storage as it is.
func (service *Service) ArchiveDiscs(ctx context.Context, title string, discs []romdomain.ArchiveDisc, out io.Writer) error {
	archive := newZipWriter(out)
	var playlist strings.Builder
	for _, disc := range discs {
		var line string
		if disc.AsTracks {
			add := func(name string, size int64, write func(io.Writer) error) error {
				entry := disc.Name + "/" + name
				if ext := strings.ToLower(path.Ext(name)); line == "" && (ext == ".cue" || ext == ".gdi") {
					line = entry
				}
				return archive.add(entry, size, write)
			}
			if err := service.writeDisc(ctx, disc.ROMFile, disc.Name, add); err != nil {
				return err
			}
			if line == "" {
				return fault.Internal(fmt.Errorf("disc %q has no sheet", disc.Name))
			}
		} else {
			line = disc.Name + path.Ext(disc.Filename)
			if err := service.copyStored(ctx, disc.ROMFile, line, archive.add); err != nil {
				return err
			}
		}
		playlist.WriteString(line + "\n")
	}
	text := playlist.String()
	if err := archive.add(title+".m3u", int64(len(text)), func(w io.Writer) error {
		_, err := io.WriteString(w, text)
		return err
	}); err != nil {
		return err
	}
	return archive.close()
}

// copyStored adds a disc's stored file as one entry.
func (service *Service) copyStored(ctx context.Context, romFile romdomain.ROMFile, name string, add AddFile) error {
	content, err := service.storage.Open(ctx, romFile.StoragePath)
	if err != nil {
		return err
	}
	defer content.Close()
	return add(name, content.Size, func(w io.Writer) error {
		_, err := io.Copy(w, content)
		return err
	})
}

// writeDisc adds a disc's sheet and track files, named after base, through add.
func (service *Service) writeDisc(ctx context.Context, romFile romdomain.ROMFile, base string, add AddFile) error {
	if romFile.Format != romdomain.FormatCHD && romFile.Format != romdomain.FormatTracks {
		return fault.Validation("this ROM is not a disc")
	}
	var chd io.ReaderAt
	var chdSize int64
	if romFile.Format == romdomain.FormatCHD {
		content, err := service.storage.Open(ctx, romFile.StoragePath)
		if err != nil {
			return err
		}
		defer content.Close()
		if chd, err = readerAt(content); err != nil {
			return err
		}
		chdSize = content.Size
		if romFile.SheetPath == nil {
			return service.images.Extract(ctx, chd, chdSize, base, add)
		}
	}

	sheet, err := service.storage.Read(ctx, sheetPath(romFile))
	if err != nil {
		return err
	}
	extension := ".cue"
	if sheetKindOf(sheet) == SheetGDI {
		extension = ".gdi"
	}
	if err := add(base+extension, int64(len(sheet)), func(writer io.Writer) error {
		_, err := writer.Write(sheet)
		return err
	}); err != nil {
		return err
	}
	tracks, err := service.repository.FindDiscTracks(ctx, romFile.ID)
	if err != nil {
		return err
	}
	// Tracks are in order, so tracks that shared an uploaded file are consecutive.
	for start := 0; start < len(tracks); {
		end, size := start, int64(0)
		for end < len(tracks) && tracks[end].Filename == tracks[start].Filename {
			size += tracks[end].Size
			end++
		}
		group := tracks[start:end]
		if err := add(group[0].Filename, size, func(writer io.Writer) error {
			for _, track := range group {
				if err := service.writeTrack(ctx, chd, chdSize, track, writer); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		start = end
	}
	return nil
}

// writeTrack writes a track's file: rebuilt from the CHD when there is one, else as stored.
func (service *Service) writeTrack(ctx context.Context, chd io.ReaderAt, chdSize int64, track romdomain.DiscTrack, out io.Writer) error {
	if chd != nil {
		return service.images.WriteTrack(ctx, chd, chdSize, track.Number, out)
	}
	if track.StoragePath == nil {
		return fault.Internal(fmt.Errorf("track %d has no stored file", track.Number))
	}
	content, err := service.storage.Open(ctx, *track.StoragePath)
	if err != nil {
		return err
	}
	defer content.Close()
	_, err = io.Copy(out, content)
	return err
}

// ISO writes a single-track MODE1 disc as an ISO image: the 2048 data bytes of each sector.
func (service *Service) ISO(ctx context.Context, romFile romdomain.ROMFile, out io.Writer) error {
	tracks, err := service.repository.FindDiscTracks(ctx, romFile.ID)
	if err != nil {
		return err
	}
	if !romdomain.ISOAvailable(tracks) {
		return fault.Validation("only a disc with a single MODE1 data track can download as an ISO")
	}
	var chd io.ReaderAt
	var chdSize int64
	if romFile.Format == romdomain.FormatCHD {
		content, err := service.storage.Open(ctx, romFile.StoragePath)
		if err != nil {
			return err
		}
		defer content.Close()
		if chd, err = readerAt(content); err != nil {
			return err
		}
		chdSize = content.Size
	}
	writer := out
	if tracks[0].SectorBytes == 2352 {
		writer = &isoWriter{out: out}
	}
	return service.writeTrack(ctx, chd, chdSize, tracks[0], writer)
}

// isoWriter keeps the 2048 user-data bytes of each raw 2352-byte MODE1 sector.
type isoWriter struct {
	out    io.Writer
	sector [2352]byte
	filled int
}

func (writer *isoWriter) Write(data []byte) (int, error) {
	written := len(data)
	for len(data) > 0 {
		copied := copy(writer.sector[writer.filled:], data)
		writer.filled += copied
		data = data[copied:]
		if writer.filled == len(writer.sector) {
			if _, err := writer.out.Write(writer.sector[16:2064]); err != nil {
				return 0, err
			}
			writer.filled = 0
		}
	}
	return written, nil
}

func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	return errors.Join(copyErr, out.Close())
}
