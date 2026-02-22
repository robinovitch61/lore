# lore

A `less`-like terminal pager, based on https://github.com/robinovitch61/viewport

`lore <file>` or `command | lore`

## Installation

```sh
# homebrew
brew install robinovitch61/tap/lore

# upgrade using homebrew
brew update && brew upgrade lore

# nix-shell
# ensure NUR is accessible (https://github.com/nix-community/NUR)
nix-shell -p nur.repos.robinovitch61.lore

# nix flakes
# ensure flake support is enabled (https://nixos.wiki/wiki/Flakes#Enable_flakes_temporarily)
nix run github:robinovitch61/nur-packages#lore

# arch linux
# PKGBUILD available at https://aur.archlinux.org/packages/lore-bin
yay -S lore-bin

# with go (https://go.dev/doc/install)
go install github.com/robinovitch61/lore@latest

# windows with winget
winget install robinovitch61.lore

# windows with scoop
scoop bucket add robinovitch61 https://github.com/robinovitch61/scoop-bucket
scoop install lore

# windows with chocolatey
choco install lore
```
