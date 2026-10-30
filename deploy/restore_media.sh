#!/usr/bin/env bash
# Restore the off-box media copy (Stage 49.10.6a).
#
# backup.sh's media_sync uploads each file of /opt/erp/media_store (product
# images, expense receipts, vendor invoice copies) as <prefix>/media/<path>.enc,
# sealed with BACKUP_ENCRYPTION_KEY - the database dump's key and cipher. This
# turns a downloaded copy of that folder back into the files:
#
#   sudo -u erp /bin/bash -c 'set -a; . /etc/erp/erp.env; set +a; \
#     bash /opt/erp/deploy/restore_media.sh /tmp/media-download /opt/erp/media_store'
#
# Every *.enc file under the first folder is decrypted to the same path, less
# ".enc", under the second. A file already there is left alone and counted, so
# a partial restore can be re-run; a file that does not decrypt (a wrong key,
# a damaged download) is named and nothing is written for it. Objects without
# ".enc" are plain copies made before 49.10.6a and are skipped.
#
# Exit 0 only when every file restored or was already present.
set -euo pipefail

: "${BACKUP_ENCRYPTION_KEY:?set BACKUP_ENCRYPTION_KEY - the media copy is encrypted with the backup key}"
export BACKUP_ENCRYPTION_KEY

if [ $# -ne 2 ]; then
  echo "usage: restore_media.sh <downloaded media folder> <target folder>" >&2
  exit 2
fi
src="${1%/}"
dest="${2%/}"
[ -d "$src" ] || { echo "no such folder: $src" >&2; exit 2; }
mkdir -p "$dest"

restored=0 present=0 failed=0
while IFS= read -r -d '' sealed; do
  rel="${sealed#"$src"/}"
  rel="${rel%.enc}"
  out="$dest/$rel"
  if [ -e "$out" ]; then
    present=$((present + 1))
    continue
  fi
  mkdir -p "$(dirname "$out")"
  # Into a side file first, so a failed decrypt never leaves half a file
  # where the ERP would serve it.
  if openssl enc -d -aes-256-cbc -pbkdf2 -in "$sealed" -out "$out.restoring" -pass env:BACKUP_ENCRYPTION_KEY 2>/dev/null; then
    mv "$out.restoring" "$out"
    restored=$((restored + 1))
  else
    rm -f "$out.restoring"
    echo "could not decrypt: $rel" >&2
    failed=$((failed + 1))
  fi
done < <(find "$src" -type f -name '*.enc' -print0)

echo "media restore: $restored restored, $present already present, $failed failed"
[ "$failed" -eq 0 ]
