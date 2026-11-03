package scraper

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/lcmps/retroxide/application/fault"
	"github.com/lcmps/retroxide/application/rom"
	romdomain "github.com/lcmps/retroxide/domain/rom"
)

const (
	SourceHasheous = "hasheous"
	SourceLibretro = "libretro"

	maxJSONBytes  = 4 << 20
	maxImageBytes = 10 << 20
	maxDATBytes   = 32 << 20
	userAgent     = "retroxide-scraper/1.0"
)

type Endpoints struct {
	Hasheous           string
	LibretroDatabase   string
	LibretroThumbnails string
}

func DefaultEndpoints() Endpoints {
	return Endpoints{
		Hasheous:           "https://hasheous.org",
		LibretroDatabase:   "https://raw.githubusercontent.com/libretro/libretro-database/master/metadat",
		LibretroThumbnails: "https://raw.githubusercontent.com/libretro-thumbnails",
	}
}

type libretroSystem struct {
	database   string
	thumbnails string
}

// libretroSystems maps platform keys to libretro's database and thumbnail repository names.
var libretroSystems = func() map[string]libretroSystem {
	databases := map[string]string{
		"nes":      "no-intro/Nintendo - Nintendo Entertainment System",
		"fds":      "no-intro/Nintendo - Family Computer Disk System",
		"snes":     "no-intro/Nintendo - Super Nintendo Entertainment System",
		"gb":       "no-intro/Nintendo - Game Boy",
		"gbc":      "no-intro/Nintendo - Game Boy Color",
		"gba":      "no-intro/Nintendo - Game Boy Advance",
		"n64":      "no-intro/Nintendo - Nintendo 64",
		"nds":      "no-intro/Nintendo - Nintendo DS",
		"vb":       "no-intro/Nintendo - Virtual Boy",
		"pokemini": "no-intro/Nintendo - Pokemon Mini",
		"sg1000":   "no-intro/Sega - SG-1000",
		"sms":      "no-intro/Sega - Master System - Mark III",
		"gg":       "no-intro/Sega - Game Gear",
		"genesis":  "no-intro/Sega - Mega Drive - Genesis",
		"32x":      "no-intro/Sega - 32X",
		"psx":      "redump/Sony - PlayStation",
		"pce":      "no-intro/NEC - PC Engine - TurboGrafx 16",
		"ngp":      "no-intro/SNK - Neo Geo Pocket",
		"ngpc":     "no-intro/SNK - Neo Geo Pocket Color",
		"ws":       "no-intro/Bandai - WonderSwan",
		"wsc":      "no-intro/Bandai - WonderSwan Color",
		"a2600":    "no-intro/Atari - 2600",
		"a7800":    "no-intro/Atari - 7800",
		"lynx":     "no-intro/Atari - Lynx",
		"jaguar":   "no-intro/Atari - Jaguar",
		"coleco":   "no-intro/Coleco - ColecoVision",
		"vectrex":  "no-intro/GCE - Vectrex",
		"intv":     "no-intro/Mattel - Intellivision",
	}
	systems := make(map[string]libretroSystem, len(databases))
	for key, database := range databases {
		// Thumbnail repositories use the database's system name with spaces as underscores.
		name := database[strings.IndexByte(database, '/')+1:]
		systems[key] = libretroSystem{database: database, thumbnails: strings.ReplaceAll(name, " ", "_")}
	}
	return systems
}()

var (
	hasheousImageID  = regexp.MustCompile(`^[A-Fa-f0-9]{16,128}$`)
	datCRC           = regexp.MustCompile(`\bcrc ([0-9A-Fa-f]{8})\b`)
	datSHA1          = regexp.MustCompile(`\bsha1 ([0-9A-Fa-f]{40})\b`)
	thumbnailUnsafe  = strings.NewReplacer("&", "_", "*", "_", "/", "_", ":", "_", "`", "_", "<", "_", ">", "_", "?", "_", "\\", "_", "|", "_")
	errUpstreamNoHit = errors.New("not found upstream")
)

// Client scrapes keyless sources: Hasheous for hash matching and metadata, libretro for canonical names and images.
type Client struct {
	http      *http.Client
	endpoints Endpoints

	mu       sync.Mutex
	datCache map[string]*datIndex
	datLoads map[string]*datLoad
}

// datLoad is an in-flight database download that concurrent lookups for the same system share.
type datLoad struct {
	done  chan struct{}
	index *datIndex
	err   error
}

var _ rom.Scraper = (*Client)(nil)

func NewClient(httpClient *http.Client, endpoints Endpoints) *Client {
	return &Client{http: httpClient, endpoints: endpoints, datCache: make(map[string]*datIndex), datLoads: make(map[string]*datLoad)}
}

func (client *Client) Scrape(ctx context.Context, query rom.ScrapeQuery) (*rom.ScrapeResult, error) {
	result := &rom.ScrapeResult{}
	matched := false
	var upstreamErrors []error

	match, err := client.lookupHasheous(ctx, query)
	if err != nil {
		upstreamErrors = append(upstreamErrors, err)
	} else if match != nil {
		matched = true
		result.Source = SourceHasheous
		result.Title = match.Name
		result.Description = match.attribute("AIDescription")
		if match.Publisher != nil {
			result.Publisher = match.Publisher.Name
		}
		if match.Signature != nil && match.Signature.Game != nil {
			result.ReleaseYear = match.Signature.Game.Year
		}
		if match.Platform != nil {
			result.Platform = romdomain.PlatformKeyForName(match.Platform.Name)
		}
		if imageID := match.attribute("Logo"); hasheousImageID.MatchString(imageID) {
			if image, err := client.fetchImage(ctx, client.endpoints.Hasheous+"/api/v1/images/"+imageID); err == nil {
				result.Cover = image
			} else if !errors.Is(err, errUpstreamNoHit) {
				upstreamErrors = append(upstreamErrors, err)
			}
		}
	}

	// A hash match names the actual system, which beats a platform guessed from the extension.
	platform := query.Platform
	if result.Platform != "" {
		platform = result.Platform
	}
	if system, ok := libretroSystems[platform]; ok {
		entry, err := client.lookupLibretro(ctx, system, query)
		if err != nil {
			upstreamErrors = append(upstreamErrors, err)
		} else if entry != nil {
			matched = true
			if result.Source == "" {
				result.Source = SourceLibretro
				result.Title = canonicalTitle(entry.name)
			}
			result.Region = entry.region
			for _, image := range []struct {
				kind   string
				target *[]byte
			}{
				{"Named_Boxarts", &result.Cover},
				{"Named_Titles", &result.TitleScreen},
				{"Named_Snaps", &result.Screenshot},
			} {
				if len(*image.target) > 0 {
					continue
				}
				content, err := client.fetchImage(ctx, client.thumbnailURL(system, image.kind, entry.name))
				if err == nil {
					*image.target = content
				} else if !errors.Is(err, errUpstreamNoHit) {
					upstreamErrors = append(upstreamErrors, err)
				}
			}
		}
	}

	if !matched {
		if len(upstreamErrors) > 0 {
			return nil, fault.UpstreamUnavailable("metadata services are unavailable, try again later", errors.Join(upstreamErrors...))
		}
		return nil, nil
	}
	return result, nil
}

type hasheousMatch struct {
	Name     string `json:"name"`
	Platform *struct {
		Name string `json:"name"`
	} `json:"platform"`
	Publisher *struct {
		Name string `json:"name"`
	} `json:"publisher"`
	Signature *struct {
		Game *struct {
			Year string `json:"year"`
		} `json:"game"`
	} `json:"signature"`
	Attributes []struct {
		AttributeName string          `json:"attributeName"`
		Value         json.RawMessage `json:"value"`
	} `json:"attributes"`
}

func (match *hasheousMatch) attribute(name string) string {
	for _, attribute := range match.Attributes {
		var value string
		if attribute.AttributeName == name && json.Unmarshal(attribute.Value, &value) == nil {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (client *Client) lookupHasheous(ctx context.Context, query rom.ScrapeQuery) (*hasheousMatch, error) {
	for _, lookup := range []struct{ kind, value string }{{"sha1", query.SHA1}, {"crc", query.CRC32}, {"md5", query.MD5}} {
		if lookup.value == "" {
			continue
		}
		body, err := client.get(ctx, client.endpoints.Hasheous+"/api/v1/Lookup/ByHash/"+lookup.kind+"/"+url.PathEscape(strings.ToLower(lookup.value)), maxJSONBytes)
		if errors.Is(err, errUpstreamNoHit) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var match hasheousMatch
		if err := json.Unmarshal(body, &match); err != nil {
			return nil, fmt.Errorf("decoding hasheous response: %w", err)
		}
		if strings.TrimSpace(match.Name) == "" {
			continue
		}
		return &match, nil
	}
	return nil, nil
}

type datEntry struct {
	name   string
	region string
}

type datIndex struct {
	bySHA1 map[string]datEntry
	byCRC  map[string]datEntry
}

func (client *Client) lookupLibretro(ctx context.Context, system libretroSystem, query rom.ScrapeQuery) (*datEntry, error) {
	index, err := client.datIndex(ctx, system)
	if err != nil {
		return nil, err
	}
	if entry, ok := index.bySHA1[strings.ToUpper(query.SHA1)]; ok && query.SHA1 != "" {
		return &entry, nil
	}
	if entry, ok := index.byCRC[strings.ToUpper(query.CRC32)]; ok && query.CRC32 != "" {
		return &entry, nil
	}
	return nil, nil
}

// datIndex returns the parsed libretro database for system, downloading it at most once at a
// time per system. The lock only guards the maps, so lookups for other systems, and cache hits,
// never wait on a download; failed downloads are not cached and are retried by the next caller.
func (client *Client) datIndex(ctx context.Context, system libretroSystem) (*datIndex, error) {
	key := system.database
	client.mu.Lock()
	if index, ok := client.datCache[key]; ok {
		client.mu.Unlock()
		return index, nil
	}
	load, inFlight := client.datLoads[key]
	if !inFlight {
		load = &datLoad{done: make(chan struct{})}
		client.datLoads[key] = load
	}
	client.mu.Unlock()

	if !inFlight {
		load.index, load.err = client.downloadDAT(ctx, system)
		client.mu.Lock()
		if load.err == nil {
			client.datCache[key] = load.index
		}
		delete(client.datLoads, key)
		client.mu.Unlock()
		close(load.done)
	}

	select {
	case <-load.done:
		return load.index, load.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (client *Client) downloadDAT(ctx context.Context, system libretroSystem) (*datIndex, error) {
	body, err := client.get(ctx, client.endpoints.LibretroDatabase+"/"+escapePath(system.database)+".dat", maxDATBytes)
	if err != nil {
		return nil, fmt.Errorf("downloading libretro database %q: %w", system.database, err)
	}
	return parseDAT(bytes.NewReader(body))
}

// parseDAT reads clrmamepro DAT files, mapping each ROM's SHA1 and CRC to its game.
func parseDAT(reader io.Reader) (*datIndex, error) {
	index := &datIndex{bySHA1: make(map[string]datEntry), byCRC: make(map[string]datEntry)}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	var current datEntry
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "game ("):
			current = datEntry{}
		case strings.HasPrefix(line, "name "):
			current.name = datString(strings.TrimPrefix(line, "name "))
		case strings.HasPrefix(line, "region "):
			current.region = datString(strings.TrimPrefix(line, "region "))
		case strings.HasPrefix(line, "rom (") && current.name != "":
			if match := datSHA1.FindStringSubmatch(line); match != nil {
				index.bySHA1[strings.ToUpper(match[1])] = current
			}
			if match := datCRC.FindStringSubmatch(line); match != nil {
				if _, exists := index.byCRC[strings.ToUpper(match[1])]; !exists {
					index.byCRC[strings.ToUpper(match[1])] = current
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading libretro database: %w", err)
	}
	return index, nil
}

func datString(raw string) string {
	if value, err := strconv.Unquote(raw); err == nil {
		return value
	}
	return strings.Trim(raw, `"`)
}

func canonicalTitle(name string) string {
	if index := strings.Index(name, " ("); index > 0 {
		return strings.TrimSpace(name[:index])
	}
	return name
}

func (client *Client) thumbnailURL(system libretroSystem, kind, gameName string) string {
	return client.endpoints.LibretroThumbnails + "/" + system.thumbnails + "/master/" + kind + "/" + url.PathEscape(thumbnailUnsafe.Replace(gameName)) + ".png"
}

func escapePath(path string) string {
	segments := strings.Split(path, "/")
	for index, segment := range segments {
		segments[index] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}

func (client *Client) fetchImage(ctx context.Context, target string) ([]byte, error) {
	body, err := client.get(ctx, target, maxImageBytes)
	if err != nil {
		return nil, err
	}
	switch http.DetectContentType(body) {
	case "image/png", "image/jpeg", "image/webp":
		return body, nil
	default:
		return nil, errUpstreamNoHit
	}
}

func (client *Client) get(ctx context.Context, target string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", userAgent)
	response, err := client.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, errUpstreamNoHit
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: unexpected status %d", target, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("GET %s: response exceeds %d bytes", target, limit)
	}
	return body, nil
}
