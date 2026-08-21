#!/usr/bin/env bash
#
# Richtet Git-Hooks und die Commit-Vorlage ein.
#
# Einmal nach dem Klonen ausführen. Hooks liegen in .githooks/ statt in
# .git/hooks/, damit sie versioniert sind — .git/hooks wird nicht mit
# geklont, und ein Hook, den niemand hat, prüft nichts.
set -euo pipefail
cd "$(dirname "$0")/.."

git config core.hooksPath .githooks
git config commit.template .gitmessage

echo "Eingerichtet:"
echo "  core.hooksPath   = .githooks"
echo "  commit.template  = .gitmessage"
echo

# Die Identität muss zum GitHub-Konto passen, sonst ordnet GitHub die Commits
# niemandem zu — sie erscheinen als grauer Platzhalter statt unter deinem Profil.
name="$(git config user.name || true)"
mail="$(git config user.email || true)"

if [ -z "$name" ] || [ -z "$mail" ]; then
  echo "  Achtung: git user.name oder user.email fehlt."
  echo "  Ohne sie ordnet GitHub deine Commits keinem Profil zu."
  echo
  echo "    git config --global user.name  \"Dein Name\""
  echo "    git config --global user.email \"deine@adresse\""
  echo
  echo "  Für eine private Adresse die GitHub-noreply verwenden:"
  echo "    https://github.com/settings/emails"
else
  echo "  Autor: $name <$mail>"
fi
