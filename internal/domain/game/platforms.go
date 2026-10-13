package game

import (
	"slices"
	"strings"
)

// Platforms that are stores or launchers (consoles and PC are platforms too: their system names).
// These names are stored in every copy, in CSV files and in redundancy checks: a Humble key is
// flagged as owned when its platform equals the store's exactly, so sources and adapters use these
// constants, never their own spelling. Never rename one (TestVocabulary_valuesNeverChange).
const (
	// PlatformSteam is Valve's store.
	PlatformSteam = "Steam"

	// PlatformEpic is the Epic Games Store.
	PlatformEpic = "Epic Games"

	// PlatformGOG is GOG.com and its Galaxy launcher.
	PlatformGOG = "GOG"

	// PlatformEA is the EA app (Origin before it).
	PlatformEA = "EA App"

	// PlatformBattlenet is Blizzard's launcher.
	PlatformBattlenet = "Battle.net"

	// PlatformUbisoft is Ubisoft Connect (Uplay before it).
	PlatformUbisoft = "Ubisoft Connect"

	// PlatformMicrosoft is the Microsoft Store, which sells for PC and Xbox under one id.
	PlatformMicrosoft = "Microsoft Store / Xbox"

	// PlatformRockstar is the Rockstar Games Launcher.
	PlatformRockstar = "Rockstar"

	// PlatformBattlestate is Battlestate Games' launcher (Escape from Tarkov).
	PlatformBattlestate = "Battlestate (Tarkov)"

	// PlatformRiot is the Riot Client.
	PlatformRiot = "Riot"

	// PlatformItch is itch.io.
	PlatformItch = "itch.io"

	// PlatformAmazon is Amazon Games (Prime Gaming).
	PlatformAmazon = "Amazon Games"

	// PlatformPlayStation is the PlayStation Store, whose games may be for several consoles.
	PlatformPlayStation = "PlayStation Store"

	// PlatformNintendo is the Nintendo eShop, whose games may be for several consoles.
	PlatformNintendo = "Nintendo eShop"
)

// Systems a copy can be played on. A console is its own platform and system; every PC store is
// played on PC. System names are stored in documents, cover file names and the CSV: never rename
// one (TestVocabulary_valuesNeverChange).
const (
	// SystemPC is the system of every PC store and launcher.
	SystemPC = "PC"

	// SystemOther is the system of a copy without a platform.
	SystemOther = "Other"

	// SystemPS5 is the PlayStation 5.
	SystemPS5 = "PS5"

	// SystemPS4 is the PlayStation 4.
	SystemPS4 = "PS4"

	// SystemPS3 is the PlayStation 3.
	SystemPS3 = "PS3"

	// SystemPS2 is the PlayStation 2.
	SystemPS2 = "PS2"

	// SystemPS1 is the original PlayStation.
	SystemPS1 = "PS1"

	// SystemPSP is the PlayStation Portable.
	SystemPSP = "PSP"

	// SystemPSVita is the PlayStation Vita.
	SystemPSVita = "PS Vita"

	// SystemXboxSeries is the Xbox Series X|S.
	SystemXboxSeries = "Xbox Series"

	// SystemXboxOne is the Xbox One.
	SystemXboxOne = "Xbox One"

	// SystemXbox360 is the Xbox 360.
	SystemXbox360 = "Xbox 360"

	// SystemXbox is the original Xbox. Every Xbox system's name starts with it (see isXbox).
	SystemXbox = "Xbox"

	// SystemSwitch is the Nintendo Switch.
	SystemSwitch = "Switch"

	// SystemWiiU is the Wii U.
	SystemWiiU = "Wii U"

	// SystemWii is the Wii.
	SystemWii = "Wii"

	// SystemGameCube is the GameCube.
	SystemGameCube = "GameCube"

	// SystemN64 is the Nintendo 64.
	SystemN64 = "N64"

	// System3DS is the Nintendo 3DS.
	System3DS = "3DS"

	// SystemDS is the Nintendo DS.
	SystemDS = "DS"

	// SystemGameBoy is the Game Boy.
	SystemGameBoy = "Game Boy"
)

// Link keys of the stores that sources import libraries from (see Links). They are stored in every
// game's links: never rename one.
const (
	// LinkSteam is the key of Steam AppIDs. Sources other than Steam's (Humble Bundle, Amazon)
	// report AppIDs too.
	LinkSteam = "steam"

	// LinkEpic is the key of Epic catalog item ids.
	LinkEpic = "epic"

	// LinkGOG is the key of GOG product ids.
	LinkGOG = "gog"

	// LinkEA is the key of EA product ids.
	LinkEA = "ea"

	// LinkBattlenet is the key of Battle.net title ids.
	LinkBattlenet = "battlenet"

	// LinkUbisoft is the key of Ubisoft space ids.
	LinkUbisoft = "ubisoft"

	// LinkXbox is the key of Microsoft Store product ids.
	LinkXbox = "xbox"

	// LinkAmazon is the key of Amazon Games product ids.
	LinkAmazon = "amazon"

	// LinkItch is the key of itch.io game ids.
	LinkItch = "itch"

	// LinkPSN is the key of PlayStation Store ids.
	LinkPSN = "psn"
)

// platformInfo is what the domain knows about one platform.
type platformInfo struct {
	// name is the platform as copies store it.
	name string

	// system is what a copy on it is played on.
	system string

	// link is the key of the store's ids in Links, empty for a platform no source links to.
	link string
}

// platforms is the one list of the stores, launchers and consoles Game Vault knows, in the order
// KnownPlatforms lists them. Adding a store or a console is one line here (and its constant).
var platforms = []platformInfo{
	// Stores and launchers.
	{PlatformSteam, SystemPC, LinkSteam},
	{PlatformEpic, SystemPC, LinkEpic},
	{PlatformGOG, SystemPC, LinkGOG},
	{PlatformEA, SystemPC, LinkEA},
	{PlatformBattlenet, SystemPC, LinkBattlenet},
	{PlatformUbisoft, SystemPC, LinkUbisoft},
	{PlatformMicrosoft, SystemPC, LinkXbox}, // sells for PC and Xbox: PC is the most likely
	{PlatformRockstar, SystemPC, ""},
	{PlatformBattlestate, SystemPC, ""},
	{PlatformRiot, SystemPC, ""},
	{PlatformItch, SystemPC, LinkItch},
	{PlatformAmazon, SystemPC, LinkAmazon},
	{PlatformPlayStation, SystemPS4, LinkPSN}, // sells for several consoles: PS4 is the most likely
	{PlatformNintendo, SystemSwitch, ""},      // likewise, the Switch

	// PC and consoles (physical formats too), each its own system.
	{SystemPC, SystemPC, ""},
	{SystemPS5, SystemPS5, ""},
	{SystemPS4, SystemPS4, ""},
	{SystemPS3, SystemPS3, ""},
	{SystemPS2, SystemPS2, ""},
	{SystemPS1, SystemPS1, ""},
	{SystemPSP, SystemPSP, ""},
	{SystemPSVita, SystemPSVita, ""},
	{SystemXboxSeries, SystemXboxSeries, ""},
	{SystemXboxOne, SystemXboxOne, ""},
	{SystemXbox360, SystemXbox360, ""},
	{SystemXbox, SystemXbox, ""},
	{SystemSwitch, SystemSwitch, ""},
	{SystemWiiU, SystemWiiU, ""},
	{SystemWii, SystemWii, ""},
	{SystemGameCube, SystemGameCube, ""},
	{SystemN64, SystemN64, ""},
	{System3DS, System3DS, ""},
	{SystemDS, SystemDS, ""},
	{SystemGameBoy, SystemGameBoy, ""},
}

// platformNamed returns the known platform p names, ignoring case and surrounding spaces.
func platformNamed(p string) (platformInfo, bool) {
	p = strings.TrimSpace(p)
	i := slices.IndexFunc(platforms, func(k platformInfo) bool { return strings.EqualFold(k.name, p) })

	if i < 0 {
		return platformInfo{}, false
	}

	return platforms[i], true
}

// IsKnownPlatform reports whether p is one of the stores, launchers or consoles Game Vault knows,
// as opposed to whatever a source names a key type (a software vendor, a course site…).
func IsKnownPlatform(p string) bool {
	_, ok := platformNamed(p)
	return ok
}

// KnownPlatforms returns the stores, launchers and consoles Game Vault knows, PC and console names
// included (a copy of the list: callers cannot change it). The web's platform tables are checked
// against it.
func KnownPlatforms() []string {
	out := make([]string, 0, len(platforms))
	for _, p := range platforms {
		out = append(out, p.name)
	}

	return out
}

// Consoles returns the consoles Game Vault knows, every known system but PC, grouped by family
// (PlayStation, Xbox, Nintendo) and newest first within each (Nintendo's home consoles, then its
// handhelds).
// Providers that map systems to a catalog's own ids check their tables against it.
func Consoles() []string {
	var out []string

	for _, p := range platforms {
		if p.name == p.system && p.system != SystemPC {
			out = append(out, p.system)
		}
	}

	return out
}

// CanonicalPlatform fixes the casing of known platforms ("steam" → "Steam") so they compare equal.
// Unknown platforms are returned trimmed but otherwise untouched.
func CanonicalPlatform(p string) string {
	if k, ok := platformNamed(p); ok {
		return k.name
	}

	return strings.TrimSpace(p)
}

// StoreLinks returns the link keys of the stores Game Vault knows, each once (a copy: callers cannot
// change it). Every source and provider that links games to a store uses one of them.
func StoreLinks() []string {
	var out []string

	for _, p := range platforms {
		if p.link != "" && !slices.Contains(out, p.link) {
			out = append(out, p.link)
		}
	}

	return out
}

// IsPCStoreLink reports whether key is the link key of a store that sells PC games. A game linked to
// one has PC art there even when the user owns it only on a console, which the console edition can
// borrow as a last resort (see Game.BorrowsPCBox). cmd/gamevault checks that every PC store's cover
// provider reads one of these keys.
func IsPCStoreLink(key string) bool {
	return key != "" && slices.ContainsFunc(platforms, func(p platformInfo) bool {
		return p.link == key && p.system == SystemPC
	})
}

// isXbox reports whether system is one of the Xbox consoles.
func isXbox(system string) bool { return strings.HasPrefix(system, SystemXbox) }
