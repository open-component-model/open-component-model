#!/usr/bin/env bash
# Demo: OCM GPG signing through the system gpg binary.
# Baseline: open-component-model#3691 (system gpg, keySource) and #3755 (tempFolder, spaced fingerprints).
#
#   ./demo.sh               setup + acts 1-8 (act 8 needs Docker)
#   ./demo.sh setup         prepare everything up front (builds, keys, FIPS image), then present acts
#   ./demo.sh 3 4           run single acts (after setup)
#   ./demo.sh cleanup       stop the demo gpg-agent, remove DEMO_DIR and the FIPS image
#
# Environment:
#   DEMO_AUTO=1       no pauses (rehearsal / CI)
#   DEMO_PINENTRY=1   act 3 unlocks the key through the real pinentry instead of presetting the passphrase
#   DEMO_DIR=...      working directory (default /tmp/ocm-gpg-demo; keep it short, gpg-agent socket paths are limited)
#   OCM_REPO=...      repository to build from (default: the repository containing this script)
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=${OCM_REPO:-$(git -C "$SCRIPT_DIR" rev-parse --show-toplevel)}
DEMO_DIR=${DEMO_DIR:-/tmp/ocm-gpg-demo}
BIN=$DEMO_DIR/bin
WORK=$DEMO_DIR/work
FIPS_IMAGE=ocm-gpg-fips-demo
PASSPHRASE='alice-demo-passphrase'
REF=ctf::$WORK/ctf//acme.org/demo/gpg:1.0.0
# shellcheck disable=SC2034 # used in the commands run through eval
FIPS_REF=ctf::/work/ctf//acme.org/demo/gpg:1.0.0

# The demo keyring. The user's ~/.gnupg is never touched.
export GNUPGHOME=$DEMO_DIR/gnupg

# ── presentation helpers ────────────────────────────────────────────────────
bold=$'\e[1m' dim=$'\e[2m' cyan=$'\e[36m' green=$'\e[32m' red=$'\e[31m' yellow=$'\e[33m' reset=$'\e[0m'
TIMEFORMAT="${yellow}⏱  %Rs${reset}"

act() { printf '\n%s━━━ %s ━━━%s\n' "$bold$cyan" "$*" "$reset"; }
say() { printf '%s# %s%s\n' "$dim" "$*" "$reset"; }
pause() { [[ ${DEMO_AUTO:-0} == 1 ]] || read -rs _ </dev/tty; }
run() {
	printf '%s$ %s%s' "$bold" "$*" "$reset"
	pause
	printf '\n'
	eval "$*"
}
run_fails() {
	printf '%s$ %s%s' "$bold" "$*" "$reset"
	pause
	printf '\n'
	if (eval "$*"); then
		printf '%s✘ unexpectedly succeeded%s\n' "$red" "$reset"
		return 1
	fi
	printf '%s✔ rejected as expected%s\n' "$green" "$reset"
}
die() {
	printf '%s%s%s\n' "$red" "$*" "$reset" >&2
	exit 1
}

# Short names for the commands shown on screen.
ocm() { "$BIN/ocm" "$@"; }
ocm-legacy() { "$BIN/ocm-legacy" "$@"; }
gpg-preset-passphrase() { "$(gpgconf --list-dirs libexecdir)/gpg-preset-passphrase" "$@"; }
# A long-running FIPS box. Files go in with docker cp, so no host directory has to be shared with Docker.
fips_box() {
	[[ $(docker inspect -f '{{.State.Running}}' "$FIPS_IMAGE" 2>/dev/null) == true ]] && return
	docker rm -f "$FIPS_IMAGE" >/dev/null 2>&1 || true
	docker run -d --name "$FIPS_IMAGE" "$FIPS_IMAGE" sleep infinity >/dev/null
	docker cp -q "$BIN/ocm-fips-linux" "$FIPS_IMAGE:/usr/local/bin/ocm"
}
fips() { docker exec "$FIPS_IMAGE" "$@"; }
# Keeps only the handler's gpg invocations and the result from --loglevel debug output.
gpg_trace() {
	grep -E 'msg="(gpg|signing with|verifying with)|signed successfully|SUCCESSFUL|Error' |
		sed -E -e 's/^time=[^ ]+ level=[A-Z]+ //' -e "s#$DEMO_DIR#\$DEMO_DIR#g" || true
}

fingerprint() { gpg --with-colons --fingerprint "$1" | awk -F: '/^fpr/ {print $10; exit}'; }
keygrip() { gpg --with-colons --with-keygrip -K "$1" | awk -F: '/^grp/ {print $10; exit}'; }
long_key_id() { gpg --with-colons -k "$1" | awk -F: '/^pub/ {print $5; exit}'; }
# The fingerprint exactly as `gpg --fingerprint` prints it, with spaces.
spaced_fingerprint() { gpg --fingerprint "$1" | sed -n '2s/^ *//p'; }

docker_ok() { [[ ${DEMO_FIPS:-1} == 1 ]] && command -v docker >/dev/null && docker info >/dev/null 2>&1; }

# ── setup ───────────────────────────────────────────────────────────────────
setup() {
	act "Setup (not part of the talk)"
	for tool in gpg gpgconf gpg-connect-agent go git; do
		command -v "$tool" >/dev/null || die "missing $tool on PATH"
	done

	if [[ -d $GNUPGHOME ]]; then gpgconf --homedir "$GNUPGHOME" --kill all 2>/dev/null || true; fi
	rm -rf "$WORK" "$GNUPGHOME" "$DEMO_DIR/tmp" "$DEMO_DIR/legacy-src"
	mkdir -p "$BIN" "$WORK" "$DEMO_DIR/tmp" "$GNUPGHOME"
	chmod 700 "$GNUPGHOME"
	# Act 3 stands in for pinentry / a YubiKey touch by presetting the passphrase in the agent.
	echo allow-preset-passphrase >"$GNUPGHOME/gpg-agent.conf"

	say "building ocm from $REPO_ROOT"
	go build -C "$REPO_ROOT/bindings/go" -o "$BIN/ocm" ./cli

	# The CLI just before #3691 signs with the pure-Go go-crypto implementation.
	local gpg_pr legacy
	gpg_pr=$(git -C "$REPO_ROOT" log --format=%H -1 --grep='system gpg binary (#3691)' || true)
	if [[ -n $gpg_pr ]]; then
		legacy=$(git -C "$REPO_ROOT" rev-parse "$gpg_pr^")
		say "building ocm-legacy (go-crypto) from ${legacy:0:9}"
		mkdir -p "$DEMO_DIR/legacy-src"
		git -C "$REPO_ROOT" archive "$legacy" bindings/go | tar -x -C "$DEMO_DIR/legacy-src"
		go build -C "$DEMO_DIR/legacy-src/bindings/go" -o "$BIN/ocm-legacy" ./cli
		rm -rf "$DEMO_DIR/legacy-src"
	else
		say "commit of #3691 not found in $REPO_ROOT; act 5 will be skipped"
		rm -f "$BIN/ocm-legacy"
	fi

	if docker_ok; then
		local arch
		case $(docker info --format '{{.Architecture}}') in
		aarch64 | arm64) arch=arm64 ;;
		*) arch=amd64 ;;
		esac
		say "building a GOFIPS140=v1.0.0 linux/$arch ocm and the FIPS-mode GnuPG image"
		GOOS=linux GOARCH=$arch CGO_ENABLED=0 GOFIPS140=v1.0.0 \
			go build -C "$REPO_ROOT/bindings/go" -o "$BIN/ocm-fips-linux" ./cli
		docker build -q -t "$FIPS_IMAGE" - >/dev/null <<-'EOF'
			FROM debian:trixie-slim
			RUN apt-get update -qq \
			 && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --no-install-recommends gnupg \
			 && rm -rf /var/lib/apt/lists/* \
			 && mkdir -p /etc/gcrypt && touch /etc/gcrypt/fips_enabled
		EOF
		docker rm -f "$FIPS_IMAGE" >/dev/null 2>&1 || true
		fips_box
	else
		say "Docker not available; act 8 (FIPS) will be skipped"
	fi

	say "generating keys in the demo keyring $GNUPGHOME"
	gpg --batch --quiet --pinentry-mode loopback --passphrase "$PASSPHRASE" \
		--quick-generate-key 'Alice Release <alice@acme.example>' rsa3072 sign 1y 2>/dev/null
	gpg --batch --quiet --pinentry-mode loopback --passphrase '' \
		--quick-generate-key 'Mallory <mallory@evil.example>' ed25519 sign 1y 2>/dev/null
	gpg --batch --check-trustdb >/dev/null 2>&1

	cd "$WORK"
	gpg --batch --pinentry-mode loopback --passphrase "$PASSPHRASE" \
		--armor --export-secret-keys alice@acme.example 2>/dev/null >alice.sec.asc
	chmod 600 alice.sec.asc
	gpg --armor --export alice@acme.example >alice.pub.asc
	gpg --armor --export mallory@evil.example >mallory.pub.asc

	local alice mallory spaced
	alice=$(fingerprint alice@acme.example)
	mallory=$(fingerprint mallory@evil.example)
	spaced=$(spaced_fingerprint alice@acme.example)

	cat >constructor.yaml <<-'EOF'
		components:
		- name: acme.org/demo/gpg
		  version: 1.0.0
		  provider:
		    name: acme.org
		  resources:
		  - name: release-notes
		    version: 1.0.0
		    type: plainText
		    input:
		      type: utf8
		      text: "signed by the system gpg binary"
	EOF

	cat >credentials.ocmconfig <<-EOF
		type: generic.config.ocm.software/v1
		configurations:
		- type: filesystem.config.ocm.software/v1alpha1
		  tempFolder: $DEMO_DIR/tmp
		- type: credentials.config.ocm.software
		  consumers:
		  - identity:
		      type: GPG/v1alpha1
		      signature: release
		    credentials:
		    - type: GPGCredentials/v1alpha1
		      privateKeyPGPFile: $WORK/alice.sec.asc
		      publicKeyPGPFile: $WORK/alice.pub.asc
		      passphrase: $PASSPHRASE
		  - identity:
		      type: GPG/v1alpha1
		      signature: legacy
		    credentials:
		    - type: GPGCredentials/v1alpha1
		      privateKeyPGPFile: $WORK/alice.sec.asc
		      publicKeyPGPFile: $WORK/alice.pub.asc
		      passphrase: $PASSPHRASE
		- type: signing.config.ocm.software/v1alpha1
		  signer:
		    type: GPGSigningConfiguration/v1alpha1
		  verifier:
		    type: GPGSigningConfiguration/v1alpha1
	EOF

	cat >wrongkey.ocmconfig <<-EOF
		type: generic.config.ocm.software/v1
		configurations:
		- type: credentials.config.ocm.software
		  consumers:
		  - identity:
		      type: GPG/v1alpha1
		      signature: release
		    credentials:
		    - type: GPGCredentials/v1alpha1
		      publicKeyPGPFile: $WORK/mallory.pub.asc
		- type: signing.config.ocm.software/v1alpha1
		  verifier:
		    type: GPGSigningConfiguration/v1alpha1
	EOF

	cat >keyring.ocmconfig <<-EOF
		type: generic.config.ocm.software/v1
		configurations:
		- type: signing.config.ocm.software/v1alpha1
		  signer:
		    type: GPGSigningConfiguration/v1alpha1
		    keySource: keyring
		    keyFingerprint: "$spaced"
		  verifier:
		    type: GPGSigningConfiguration/v1alpha1
		    keySource: keyring
		    keyFingerprint: "$spaced"
	EOF

	keyring_verifier() {
		cat <<-EOF
			type: generic.config.ocm.software/v1
			configurations:
			- type: signing.config.ocm.software/v1alpha1
			  verifier:
			    type: GPGSigningConfiguration/v1alpha1
			    keySource: keyring
			    keyFingerprint: $1
		EOF
	}
	keyring_verifier "$(long_key_id alice@acme.example)" >keyring-longid.ocmconfig
	keyring_verifier "$mallory" >keyring-mallory.ocmconfig

	cat >keyring-with-keyfile.ocmconfig <<-EOF
		type: generic.config.ocm.software/v1
		configurations:
		- type: credentials.config.ocm.software
		  consumers:
		  - identity:
		      type: GPG/v1alpha1
		      signature: keyring
		    credentials:
		    - type: GPGCredentials/v1alpha1
		      publicKeyPGPFile: $WORK/alice.pub.asc
		- type: signing.config.ocm.software/v1alpha1
		  verifier:
		    type: GPGSigningConfiguration/v1alpha1
		    keySource: keyring
		    keyFingerprint: $alice
	EOF

	# Paths as mounted into the FIPS container (WORK -> /work).
	cat >fips.ocmconfig <<-EOF
		type: generic.config.ocm.software/v1
		configurations:
		- type: credentials.config.ocm.software
		  consumers:
		  - identity:
		      type: GPG/v1alpha1
		      signature: fips
		    credentials:
		    - type: GPGCredentials/v1alpha1
		      privateKeyPGPFile: /work/alice.sec.asc
		      publicKeyPGPFile: /work/alice.pub.asc
		      passphrase: $PASSPHRASE
		  - identity:
		      type: GPG/v1alpha1
		      signature: release
		    credentials:
		    - type: GPGCredentials/v1alpha1
		      publicKeyPGPFile: /work/alice.pub.asc
		- type: signing.config.ocm.software/v1alpha1
		  signer:
		    type: GPGSigningConfiguration/v1alpha1
		  verifier:
		    type: GPGSigningConfiguration/v1alpha1
	EOF

	ocm add cv --repository "ctf::$WORK/ctf" --constructor constructor.yaml >/dev/null
	printf '%s✔ ready: %s%s\n' "$green" "$DEMO_DIR" "$reset"
}

# ── acts ────────────────────────────────────────────────────────────────────
act1() {
	act "1 · Default keySource: credentials — the key comes from .ocmconfig, gpg does the crypto"
	say "REF=$REF"
	run 'cat credentials.ocmconfig'
	say "debug log, filtered to the handler's gpg calls: fresh GnuPG home, passphrase on stdin (--passphrase-fd 0), never on argv"
	run 'time ocm sign cv "$REF" --signature release --config credentials.ocmconfig --loglevel debug 2>&1 | gpg_trace'
	say "the GnuPG home lived under tempFolder; its gpg-agent was killed and the directory removed:"
	run 'ls -A "$DEMO_DIR/tmp"'
	run 'time ocm verify cv "$REF" --signature release --config credentials.ocmconfig'
	run 'ocm get cv "$REF" -o yaml | sed -n "/signatures:/,/END PGP/p"'
}

act2() {
	act "2 · A different public key does not verify"
	run_fails 'ocm verify cv "$REF" --signature release --config wrongkey.ocmconfig'
}

act3() {
	act "3 · keySource: keyring — your GnuPG keyring and gpg-agent (YubiKey, pinentry, passphrase cache)"
	run 'echo "GNUPGHOME=$GNUPGHOME"; gpg --list-secret-keys --keyid-format long'
	gpg-connect-agent /bye >/dev/null 2>&1
	if [[ ${DEMO_PINENTRY:-0} == 1 ]]; then
		say "the first sign below pops up pinentry; passphrase: $PASSPHRASE"
	else
		say "unlock the key in gpg-agent once — stands in for pinentry or a hardware-token touch:"
		run 'gpg-preset-passphrase --preset -P "$PASSPHRASE" "$(keygrip alice@acme.example)"'
	fi
	say "copy the fingerprint as gpg prints it — spaces and 0x are accepted (#3755):"
	run 'gpg --fingerprint alice@acme.example'
	say "no key files, no passphrase — nothing secret in the config:"
	run 'cat keyring.ocmconfig'
	run 'time ocm sign cv "$REF" --signature keyring --config keyring.ocmconfig --loglevel debug 2>&1 | gpg_trace'
	run 'time ocm verify cv "$REF" --signature keyring --config keyring.ocmconfig --loglevel debug 2>&1 | gpg_trace'
	say "verify runs with --trust-model always (the pin is the trust decision) and --no-auto-key-retrieve (no network)"
}

act4() {
	act "4 · Keyring verification accepts exactly one pinned key"
	say "a long key ID could collide among all keys in a keyring — a full fingerprint is required:"
	run 'grep keyFingerprint keyring-longid.ocmconfig'
	run_fails 'ocm verify cv "$REF" --signature keyring --config keyring-longid.ocmconfig'
	say "Mallory's key is in the same keyring and trusted — but it is not the pinned key:"
	run_fails 'ocm verify cv "$REF" --signature keyring --config keyring-mallory.ocmconfig'
	say "key files together with keySource: keyring are ambiguous and rejected:"
	run_fails 'ocm verify cv "$REF" --signature keyring --config keyring-with-keyfile.ocmconfig'
}

act5() {
	act "5 · Signatures made by the old go-crypto implementation still verify"
	if [[ ! -x $BIN/ocm-legacy ]]; then
		say "skipped: no ocm-legacy build"
		return
	fi
	say "ocm-legacy is the CLI just before #3691 — hide gpg from it to show it signs in pure Go:"
	run 'PATH=/nonexistent ocm-legacy sign cv "$REF" --signature legacy --config credentials.ocmconfig 2>&1 | tail -1'
	run 'ocm verify cv "$REF" --signature legacy --config credentials.ocmconfig --loglevel debug 2>&1 | gpg_trace'
}

act6() {
	act "6 · No silent fallback to Go crypto (breaking: GnuPG >= 2.2 is required)"
	run_fails 'PATH=/nonexistent ocm verify cv "$REF" --signature release --config credentials.ocmconfig'
	say "not even with Go's FIPS mode switched off:"
	run_fails 'GODEBUG=fips140=off PATH=/nonexistent ocm verify cv "$REF" --signature release --config credentials.ocmconfig'
}

act7() {
	act "7 · A key revoked in your keyring fails keyring verification"
	run 'sed "s/^:-----/-----/" "$GNUPGHOME/openpgp-revocs.d/$(fingerprint alice@acme.example).rev" | gpg --batch --import'
	run_fails 'ocm verify cv "$REF" --signature keyring --config keyring.ocmconfig'
	say "keySource: credentials is hermetic — its trust anchor is the configured key file, not your keyring:"
	run 'ocm verify cv "$REF" --signature release --config credentials.ocmconfig'
}

act8() {
	act "8 · FIPS 140-3: GOFIPS140 CLI + GnuPG on a FIPS-mode libgcrypt (debian:trixie-slim)"
	if ! docker_ok || [[ ! -x $BIN/ocm-fips-linux ]]; then
		say "skipped: needs Docker and the FIPS build from setup"
		return
	fi
	fips_box
	run 'go version -m "$BIN/ocm-fips-linux" | grep -E "GOFIPS140|GOOS|GOARCH"'
	run 'fips gpgconf --show-versions | grep -iE "^\* (GnuPG|Libgcrypt)|fips-mode"'
	say "ship the CTF (with the signatures made on the laptop) and the key files into the FIPS box:"
	run 'docker cp -q "$WORK/." "$FIPS_IMAGE:/work"'
	say "all OpenPGP crypto, including unlocking the passphrase-protected key, runs in libgcrypt:"
	run 'fips ocm sign cv "$FIPS_REF" --signature fips --config /work/fips.ocmconfig 2>&1 | tail -1'
	run 'fips ocm verify cv "$FIPS_REF" --signature fips --config /work/fips.ocmconfig'
	say "the signature made on the laptop in act 1 verifies in FIPS mode, too:"
	run 'fips ocm verify cv "$FIPS_REF" --signature release --config /work/fips.ocmconfig'
}

cleanup() {
	act "Cleanup"
	if [[ -d $GNUPGHOME ]]; then gpgconf --homedir "$GNUPGHOME" --kill all 2>/dev/null || true; fi
	rm -rf "$DEMO_DIR"
	if command -v docker >/dev/null; then
		docker rm -f "$FIPS_IMAGE" >/dev/null 2>&1 || true
		docker rmi -f "$FIPS_IMAGE" >/dev/null 2>&1 || true
	fi
	printf '%s✔ removed %s%s\n' "$green" "$DEMO_DIR" "$reset"
}

main() {
	local steps=("$@")
	[[ ${#steps[@]} -gt 0 ]] || steps=(all)
	for step in "${steps[@]}"; do
		case $step in
		all)
			setup
			for n in 1 2 3 4 5 6 7 8; do "act$n"; done
			;;
		setup) setup ;;
		[1-8])
			[[ -f $WORK/keyring.ocmconfig ]] || die "run ./demo.sh setup first"
			cd "$WORK"
			"act$step"
			;;
		cleanup) cleanup ;;
		*) die "unknown step $step (all | setup | 1-8 | cleanup)" ;;
		esac
	done
}

main "$@"
