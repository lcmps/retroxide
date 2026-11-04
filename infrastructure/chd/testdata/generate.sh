#!/bin/sh
# Regenerates the CHD test fixtures with MAME's chdman (run from this directory; needs Docker).
# Sources come from fixtures_gen_test.go; expected outputs are chdman extractcd's.
set -eu
here=$(pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/src"
(cd .. && CHD_FIXTURE_SOURCES="$work/src" go test -tags chdfixtures -run TestGenerateFixtureSources -count=1 . >/dev/null)
docker build -q -t retroxide-chdman - >/dev/null <<'DOCKERFILE'
FROM debian:stable-slim
RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends mame-tools && rm -rf /var/lib/apt/lists/*
ENTRYPOINT ["chdman"]
DOCKERFILE
chdman() { docker run --rm -u "$(id -u):$(id -g)" -v "$work:/w" -w /w retroxide-chdman "$@" >/dev/null; }
for codec in cdlz cdzl cdfl cdzs; do chdman createcd -i src/Game.cue -o "cd-$codec.chd" -c "$codec"; done
chdman createcd -i src/Disc.gdi -o gd.chd
for codec in zlib lzma huff flac zstd none; do chdman createraw -i src/disk.raw -o "raw-$codec.chd" -hs 4096 -us 512 -c "$codec"; done
mkdir -p "$work/expected/cd" "$work/expected/gd"
chdman extractcd -i cd-cdlz.chd -o expected/cd/Game.cue -sb
chdman extractcd -i gd.chd -o expected/gd/Disc.gdi
rm -rf "$here/chd" "$here/expected" && mkdir -p "$here/chd"
cp "$work"/*.chd "$here/chd/"
cp -r "$work/expected" "$here/expected"
cp "$work/src/disk.raw" "$here/expected/disk.raw"
# Track files are kept only as hashes: the test compares extracted tracks against them.
for dir in cd gd; do
	(cd "$here/expected/$dir" && find . -maxdepth 1 \( -name '*.bin' -o -name '*.raw' \) -printf '%f\0' | sort -z | xargs -0 sha1sum -- > SHA1SUMS)
	find "$here/expected/$dir" -maxdepth 1 \( -name '*.bin' -o -name '*.raw' \) -delete
done
echo "fixtures regenerated"
