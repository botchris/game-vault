package game

import (
	"slices"
	"strings"
	"unicode"
)

// SystemKind says what sort of machine a system is.
type SystemKind string

// Values of SystemKind.
const (
	// SystemKindPC is PC, its own kind.
	SystemKindPC SystemKind = "pc"

	// SystemKindHome is a home console.
	SystemKindHome SystemKind = "home"

	// SystemKindHandheld is a handheld console.
	SystemKindHandheld SystemKind = "handheld"

	// SystemKindComputer is a classic home computer.
	SystemKindComputer SystemKind = "computer"
)

// System is one game system of the catalog: what a copy can be played on.
type System struct {
	// Name is the stored name: copies, editions, cover file names and CSV files use it, so it never
	// changes (TestSystems_namesNeverChange).
	Name string

	// Maker is who made it ("Nintendo", "Sega"), as the picker groups systems.
	Maker string

	// Kind says whether it is a home console, a handheld or a computer.
	Kind SystemKind

	// Year is the year of its first release anywhere.
	Year int

	// Aliases are its regional names, abbreviations and spellings ("Genesis", "Sega Mega Drive").
	Aliases []string

	// Modern reports that Steam's PC version of a game usually is the same game: PC and the
	// consoles of the PS4 generation or later that share most games with PC.
	Modern bool

	// Short is the badge label when it differs from the name ("MD", "GBA"); empty: the name.
	Short string
}

// catalog is every system Game Vault knows, in display order: PC, then by maker; within a maker,
// home consoles, then handhelds, then computers, each newest first. Adding a system is one entry
// here plus one line in pinnedSystemNames, and a decision in each provider's table.
var catalog = []System{
	// PC.
	{
		Name:    SystemPC,
		Maker:   "PC",
		Kind:    SystemKindPC,
		Year:    1981,
		Aliases: []string{"Windows", "Microsoft Windows", "Windows PC", "PC (Microsoft Windows)", "PC (Windows)", "DOS", "MS-DOS", "Linux", "Mac", "macOS", "Mac OS", "Mac OS X", "OS X", "Steam Deck", "SteamOS", "IBM PC", "PC DVD", "PC CD", "PC CD-ROM", "PC DVD-ROM"},
		Modern:  true,
	},
	// Sony.
	{
		Name:    SystemPS5,
		Maker:   "Sony",
		Kind:    SystemKindHome,
		Year:    2020,
		Aliases: []string{"PlayStation 5", "PS 5", "Sony PlayStation 5", "PS5 Pro", "PS5 Slim", "PlayStation 5 Pro", "PlayStation 5 Slim"},
		Modern:  true,
	},
	{
		Name:    SystemPS4,
		Maker:   "Sony",
		Kind:    SystemKindHome,
		Year:    2013,
		Aliases: []string{"PlayStation 4", "PS 4", "Sony PlayStation 4", "PS4 Pro", "PS4 Slim", "PlayStation 4 Pro", "PlayStation 4 Slim"},
		Modern:  true,
	},
	{
		Name:    SystemPS3,
		Maker:   "Sony",
		Kind:    SystemKindHome,
		Year:    2006,
		Aliases: []string{"PlayStation 3", "PS 3", "Sony PlayStation 3", "PS3 Slim", "PS3 Super Slim"},
	},
	{
		Name:    SystemPS2,
		Maker:   "Sony",
		Kind:    SystemKindHome,
		Year:    2000,
		Aliases: []string{"PlayStation 2", "PS 2", "Sony PlayStation 2", "PS2 Slim", "PStwo", "PS2 Slimline"},
	},
	{
		Name:    SystemPS1,
		Maker:   "Sony",
		Kind:    SystemKindHome,
		Year:    1994,
		Aliases: []string{"PlayStation", "PlayStation 1", "PlayStation One", "PSX", "PS one", "PSone", "Sony PlayStation", "Sony PlayStation 1"},
	},
	{
		Name:    SystemPSVita,
		Maker:   "Sony",
		Kind:    SystemKindHandheld,
		Year:    2011,
		Aliases: []string{"PlayStation Vita", "PSV", "PSVita", "PS Vita TV", "PlayStation Vita TV", "Sony PS Vita", "Sony PlayStation Vita"},
		Short:   "Vita",
	},
	{
		Name:    SystemPSP,
		Maker:   "Sony",
		Kind:    SystemKindHandheld,
		Year:    2004,
		Aliases: []string{"PlayStation Portable", "Sony PSP", "PSP Go", "PSP Street", "PSP-1000", "PSP-2000", "PSP-3000"},
	},
	// Microsoft.
	{
		Name:    SystemXboxSeries,
		Maker:   "Microsoft",
		Kind:    SystemKindHome,
		Year:    2020,
		Aliases: []string{"Xbox Series X", "Xbox Series S", "Xbox Series X|S", "Xbox Series X/S", "Series X|S", "Series X/S", "Xbox Series X S", "XSX", "Microsoft Xbox Series X"},
		Modern:  true,
		Short:   "XS",
	},
	{
		Name:    SystemXboxOne,
		Maker:   "Microsoft",
		Kind:    SystemKindHome,
		Year:    2013,
		Aliases: []string{"Xbox One S", "Xbox One X", "XB1", "Microsoft Xbox One", "XboxOne", "Xbone"},
		Modern:  true,
		Short:   "XONE",
	},
	{
		Name:    SystemXbox360,
		Maker:   "Microsoft",
		Kind:    SystemKindHome,
		Year:    2005,
		Aliases: []string{"X360", "XBOX360", "Xbox360", "Microsoft Xbox 360", "Xbox 360 S", "Xbox 360 E", "XB360"},
		Short:   "X360",
	},
	{
		Name:    SystemXbox,
		Maker:   "Microsoft",
		Kind:    SystemKindHome,
		Year:    2001,
		Aliases: []string{"Original Xbox", "Xbox (original)", "Xbox Classic", "OG Xbox", "Microsoft Xbox"},
		Short:   "XBOX",
	},
	// Nintendo.
	{
		Name:    "Switch 2",
		Maker:   "Nintendo",
		Kind:    SystemKindHome,
		Year:    2025,
		Aliases: []string{"Nintendo Switch 2", "NS2", "Switch2", "Nintendo Switch 2 Edition"},
		Modern:  true,
		Short:   "NSW2",
	},
	{
		Name:    SystemSwitch,
		Maker:   "Nintendo",
		Kind:    SystemKindHome,
		Year:    2017,
		Aliases: []string{"Nintendo Switch", "Nintendo Switch OLED", "Switch OLED", "Switch Lite", "Nintendo Switch Lite"},
		Modern:  true,
		Short:   "NSW",
	},
	{
		Name:    SystemWiiU,
		Maker:   "Nintendo",
		Kind:    SystemKindHome,
		Year:    2012,
		Aliases: []string{"Nintendo Wii U", "WiiU", "Wii-U"},
		Short:   "WiiU",
	},
	{
		Name:    SystemWii,
		Maker:   "Nintendo",
		Kind:    SystemKindHome,
		Year:    2006,
		Aliases: []string{"Nintendo Wii", "Wii Mini"},
	},
	{
		Name:    SystemGameCube,
		Maker:   "Nintendo",
		Kind:    SystemKindHome,
		Year:    2001,
		Aliases: []string{"Nintendo GameCube", "Game Cube", "Nintendo Game Cube", "GCN", "NGC", "GC"},
		Short:   "GC",
	},
	{
		Name:    "Nintendo 64DD",
		Maker:   "Nintendo",
		Kind:    SystemKindHome,
		Year:    1999,
		Aliases: []string{"64DD", "N64DD", "Nintendo 64 Disk Drive", "Nintendo 64 Disk Drive (64DD)"},
		Short:   "64DD",
	},
	{
		Name:    SystemN64,
		Maker:   "Nintendo",
		Kind:    SystemKindHome,
		Year:    1996,
		Aliases: []string{"Nintendo 64", "Nintendo64", "N 64", "Ultra 64"},
	},
	{
		Name:    "SNES",
		Maker:   "Nintendo",
		Kind:    SystemKindHome,
		Year:    1990,
		Aliases: []string{"Super Nintendo", "Super Nintendo Entertainment System", "Super NES", "Super Famicom", "Super Family Computer", "SFC"},
	},
	{
		Name:    "Famicom Disk System",
		Maker:   "Nintendo",
		Kind:    SystemKindHome,
		Year:    1986,
		Aliases: []string{"Family Computer Disk System", "Famicom Disk", "Disk System", "Nintendo Famicom Disk System"},
		Short:   "FDS",
	},
	{
		Name:    "NES",
		Maker:   "Nintendo",
		Kind:    SystemKindHome,
		Year:    1983,
		Aliases: []string{"Nintendo Entertainment System", "Famicom", "Family Computer", "Nintendo Famicom", "Famicom (Nintendo)", "Nintendo NES"},
	},
	{
		Name:    System3DS,
		Maker:   "Nintendo",
		Kind:    SystemKindHandheld,
		Year:    2011,
		Aliases: []string{"Nintendo 3DS", "New 3DS", "New Nintendo 3DS", "3DS XL", "New 3DS XL", "N3DS", "2DS", "Nintendo 2DS", "New 2DS XL", "New Nintendo 2DS XL"},
	},
	{
		Name:    SystemDS,
		Maker:   "Nintendo",
		Kind:    SystemKindHandheld,
		Year:    2004,
		Aliases: []string{"Nintendo DS", "NDS", "DS Lite", "Nintendo DS Lite", "DSi", "Nintendo DSi", "DSi XL", "NDS Lite"},
		Short:   "NDS",
	},
	{
		Name:    "Game Boy Advance",
		Maker:   "Nintendo",
		Kind:    SystemKindHandheld,
		Year:    2001,
		Aliases: []string{"Nintendo Game Boy Advance", "GBA SP", "Game Boy Advance SP", "Game Boy Micro", "Gameboy Advance"},
		Short:   "GBA",
	},
	{
		Name:    "Pokémon Mini",
		Maker:   "Nintendo",
		Kind:    SystemKindHandheld,
		Year:    2001,
		Aliases: []string{"Pokemon Mini", "Nintendo Pokémon Mini", "Nintendo Pokemon Mini", "Pokemon mini"},
	},
	{
		Name:    "Game Boy Color",
		Maker:   "Nintendo",
		Kind:    SystemKindHandheld,
		Year:    1998,
		Aliases: []string{"Nintendo Game Boy Color", "Gameboy Color"},
		Short:   "GBC",
	},
	{
		Name:    "Virtual Boy",
		Maker:   "Nintendo",
		Kind:    SystemKindHandheld,
		Year:    1995,
		Aliases: []string{"Nintendo Virtual Boy", "VirtualBoy"},
		Short:   "VB",
	},
	{
		Name:    SystemGameBoy,
		Maker:   "Nintendo",
		Kind:    SystemKindHandheld,
		Year:    1989,
		Aliases: []string{"Nintendo Game Boy", "Gameboy", "Game Boy Original", "Game Boy Classic", "Game Boy Pocket", "Game Boy Light", "Gameboy Pocket"},
		Short:   "GB",
	},
	{
		Name:    "Game & Watch",
		Maker:   "Nintendo",
		Kind:    SystemKindHandheld,
		Year:    1980,
		Aliases: []string{"Game and Watch", "Nintendo Game & Watch", "Nintendo Game and Watch", "GnW"},
		Short:   "G&W",
	},
	// Sega.
	{
		Name:    "Dreamcast",
		Maker:   "Sega",
		Kind:    SystemKindHome,
		Year:    1998,
		Aliases: []string{"Sega Dreamcast"},
		Short:   "DC",
	},
	{
		Name:    "32X",
		Maker:   "Sega",
		Kind:    SystemKindHome,
		Year:    1994,
		Aliases: []string{"Sega 32X", "Mega 32X", "Sega Mega 32X", "Sega Genesis 32X", "Super 32X", "Sega Super 32X"},
	},
	{
		Name:    "Saturn",
		Maker:   "Sega",
		Kind:    SystemKindHome,
		Year:    1994,
		Aliases: []string{"Sega Saturn"},
		Short:   "SAT",
	},
	{
		Name:    "Pico",
		Maker:   "Sega",
		Kind:    SystemKindHome,
		Year:    1993,
		Aliases: []string{"Sega Pico", "Sega Mega Pico", "Mega Pico", "Kids Computer Pico"},
		Short:   "PICO",
	},
	{
		Name:    "Mega-CD / Sega CD",
		Maker:   "Sega",
		Kind:    SystemKindHome,
		Year:    1991,
		Aliases: []string{"Sega CD", "Mega-CD", "Mega CD", "Sega Mega-CD", "Sega Mega CD", "SegaCD"},
		Short:   "MCD",
	},
	{
		Name:    "Mega Drive / Genesis",
		Maker:   "Sega",
		Kind:    SystemKindHome,
		Year:    1988,
		Aliases: []string{"Sega Genesis", "Sega Mega Drive", "Mega Drive", "Genesis", "Sega Megadrive", "Megadrive", "Sega Mega Drive/Genesis", "Mega Drive/Genesis", "Sega Nomad", "Genesis Nomad", "Sega Genesis Nomad"},
		Short:   "MD",
	},
	{
		Name:    "Master System",
		Maker:   "Sega",
		Kind:    SystemKindHome,
		Year:    1985,
		Aliases: []string{"Sega Master System", "Mark III", "Sega Mark III", "Sega Master System / Mark III", "Master System / Mark III", "Sega Master System/Mark III", "SMS"},
		Short:   "SMS",
	},
	{
		Name:    "SG-1000",
		Maker:   "Sega",
		Kind:    SystemKindHome,
		Year:    1983,
		Aliases: []string{"SG1000", "Sega SG-1000", "SEGA SG-1000", "SC-3000", "Sega SC-3000"},
		Short:   "SG1K",
	},
	{
		Name:    "Game Gear",
		Maker:   "Sega",
		Kind:    SystemKindHandheld,
		Year:    1990,
		Aliases: []string{"Sega Game Gear", "GameGear"},
		Short:   "GG",
	},
	// Atari.
	{
		Name:    "Atari Jaguar CD",
		Maker:   "Atari",
		Kind:    SystemKindHome,
		Year:    1995,
		Aliases: []string{"Jaguar CD"},
		Short:   "JCD",
	},
	{
		Name:    "Atari Jaguar",
		Maker:   "Atari",
		Kind:    SystemKindHome,
		Year:    1993,
		Aliases: []string{"Jaguar", "Atari Jaguar 64"},
		Short:   "JAG",
	},
	{
		Name:    "Atari 7800",
		Maker:   "Atari",
		Kind:    SystemKindHome,
		Year:    1986,
		Aliases: []string{"Atari 7800 ProSystem", "Atari ProSystem"},
		Short:   "7800",
	},
	{
		Name:    "Atari 5200",
		Maker:   "Atari",
		Kind:    SystemKindHome,
		Year:    1982,
		Aliases: []string{"Atari 5200 SuperSystem", "Atari 5200 Super System"},
		Short:   "5200",
	},
	{
		Name:    "Atari 2600",
		Maker:   "Atari",
		Kind:    SystemKindHome,
		Year:    1977,
		Aliases: []string{"Atari VCS", "Atari Video Computer System", "Atari 2600 VCS"},
		Short:   "2600",
	},
	{
		Name:    "Atari Lynx",
		Maker:   "Atari",
		Kind:    SystemKindHandheld,
		Year:    1989,
		Aliases: []string{"Lynx", "Atari Lynx II", "Epyx Handy"},
		Short:   "LYNX",
	},
	{
		Name:    "Atari ST",
		Maker:   "Atari",
		Kind:    SystemKindComputer,
		Year:    1985,
		Aliases: []string{"Atari ST/STE", "Atari STE", "Atari 520ST", "Atari 1040ST"},
		Short:   "ST",
	},
	{
		Name:    "Atari 8-bit",
		Maker:   "Atari",
		Kind:    SystemKindComputer,
		Year:    1979,
		Aliases: []string{"Atari 800", "Atari 400", "Atari XL", "Atari XE", "Atari 800XL", "Atari 130XE", "Atari XEGS", "Atari 8 bit", "Atari 8-bit family"},
		Short:   "A8",
	},
	// NEC.
	{
		Name:    "PC-FX",
		Maker:   "NEC",
		Kind:    SystemKindHome,
		Year:    1994,
		Aliases: []string{"NEC PC-FX", "PCFX"},
		Short:   "PCFX",
	},
	{
		Name:    "SuperGrafx",
		Maker:   "NEC",
		Kind:    SystemKindHome,
		Year:    1989,
		Aliases: []string{"PC Engine SuperGrafx", "NEC SuperGrafx"},
		Short:   "SGFX",
	},
	{
		Name:    "PC Engine CD / TurboGrafx-CD",
		Maker:   "NEC",
		Kind:    SystemKindHome,
		Year:    1988,
		Aliases: []string{"PC Engine CD", "TurboGrafx-CD", "TurboGrafx CD", "Turbografx-16/PC Engine CD", "CD-ROM2", "PC Engine CD-ROM2", "Super CD-ROM2", "PC Engine Super CD-ROM2", "Arcade CD-ROM2", "PC Engine Duo", "TurboDuo", "TurboGrafx Duo", "PC Engine Duo-R", "PC Engine Duo-RX"},
		Short:   "PCECD",
	},
	{
		Name:    "PC Engine / TurboGrafx-16",
		Maker:   "NEC",
		Kind:    SystemKindHome,
		Year:    1987,
		Aliases: []string{"PC Engine", "TurboGrafx-16", "TurboGrafx 16", "TurboGrafx16", "TG16", "TG-16", "TurboGrafx-16/PC Engine", "PC Engine / TurboGrafx 16", "PC Engine CoreGrafx", "CoreGrafx", "PC Engine GT", "TurboExpress", "PC Engine LT"},
		Short:   "PCE",
	},
	{
		Name:    "PC-98",
		Maker:   "NEC",
		Kind:    SystemKindComputer,
		Year:    1982,
		Aliases: []string{"PC-9800", "PC-9800 Series", "NEC PC-98", "NEC PC-9800", "PC-9801", "PC98", "PC-9821", "NEC PC-9801"},
		Short:   "PC98",
	},
	{
		Name:    "PC-88",
		Maker:   "NEC",
		Kind:    SystemKindComputer,
		Year:    1981,
		Aliases: []string{"PC-8800", "PC-8800 Series", "NEC PC-88", "NEC PC-8801", "PC-8801", "PC88", "PC-8801 Series"},
		Short:   "PC88",
	},
	// SNK.
	{
		Name:    "Neo Geo CD",
		Maker:   "SNK",
		Kind:    SystemKindHome,
		Year:    1994,
		Aliases: []string{"NeoGeo CD", "Neo-Geo CD", "SNK Neo Geo CD", "NGCD"},
		Short:   "NGCD",
	},
	{
		Name:    "Neo Geo AES",
		Maker:   "SNK",
		Kind:    SystemKindHome,
		Year:    1990,
		Aliases: []string{"Neo Geo", "NeoGeo", "Neo-Geo", "Neo Geo MVS", "Neo-Geo AES", "Neo Geo Advanced Entertainment System", "SNK Neo Geo", "NGAES", "Neo Geo Multi Video System"},
		Short:   "NEO",
	},
	{
		Name:    "Neo Geo Pocket Color",
		Maker:   "SNK",
		Kind:    SystemKindHandheld,
		Year:    1999,
		Aliases: []string{"NGPC", "Neo-Geo Pocket Color", "NeoGeo Pocket Color", "SNK Neo Geo Pocket Color"},
		Short:   "NGPC",
	},
	{
		Name:    "Neo Geo Pocket",
		Maker:   "SNK",
		Kind:    SystemKindHandheld,
		Year:    1998,
		Aliases: []string{"NGP", "Neo-Geo Pocket", "NeoGeo Pocket", "SNK Neo Geo Pocket"},
		Short:   "NGP",
	},
	// Bandai.
	{
		Name:    "Playdia",
		Maker:   "Bandai",
		Kind:    SystemKindHome,
		Year:    1994,
		Aliases: []string{"Bandai Playdia"},
	},
	{
		Name:    "WonderSwan Color",
		Maker:   "Bandai",
		Kind:    SystemKindHandheld,
		Year:    2000,
		Aliases: []string{"WSC", "Wonder Swan Color", "Bandai WonderSwan Color", "SwanCrystal", "Swan Crystal"},
		Short:   "WSC",
	},
	{
		Name:    "WonderSwan",
		Maker:   "Bandai",
		Kind:    SystemKindHandheld,
		Year:    1999,
		Aliases: []string{"Wonder Swan", "Bandai WonderSwan"},
		Short:   "WS",
	},
	// Acorn.
	{
		Name:    "Acorn Archimedes",
		Maker:   "Acorn",
		Kind:    SystemKindComputer,
		Year:    1987,
		Aliases: []string{"Archimedes", "Acorn Archimedes A3000", "Acorn A3000", "Acorn A5000", "Acorn A500", "Acorn RISC PC", "RISC PC", "Acorn Risc PC", "Acorn A3020", "Acorn A310"},
	},
	{
		Name:    "Acorn Electron",
		Maker:   "Acorn",
		Kind:    SystemKindComputer,
		Year:    1983,
		Aliases: []string{},
		Short:   "ELEC",
	},
	{
		Name:    "BBC Micro",
		Maker:   "Acorn",
		Kind:    SystemKindComputer,
		Year:    1981,
		Aliases: []string{"BBC Microcomputer System", "BBC Micro Model B", "Acorn BBC Micro", "BBC Model B", "BBC B", "BBC Master", "BBC Master Compact", "Acorn BBC Master", "BBC Microcomputer", "Acorn BBC Model B"},
		Short:   "BBC",
	},
	// Amstrad.
	{
		Name:    "Amstrad GX4000",
		Maker:   "Amstrad",
		Kind:    SystemKindHome,
		Year:    1990,
		Aliases: []string{"GX4000", "Amstrad GX-4000", "Amstrad CPC Plus console"},
	},
	{
		Name:    "Amstrad CPC",
		Maker:   "Amstrad",
		Kind:    SystemKindComputer,
		Year:    1984,
		Aliases: []string{"Amstrad CPC 464", "Amstrad CPC 6128", "Amstrad CPC 664", "CPC 464", "CPC 6128", "ACPC", "Schneider CPC", "Amstrad CPC+"},
		Short:   "CPC",
	},
	// Apple.
	{
		Name:    "Apple Pippin",
		Maker:   "Apple",
		Kind:    SystemKindHome,
		Year:    1996,
		Aliases: []string{"Pippin", "Bandai Pippin", "Bandai Pippin Atmark", "Pippin Atmark", "Apple Bandai Pippin"},
	},
	{
		Name:    "Mac (classic)",
		Maker:   "Apple",
		Kind:    SystemKindComputer,
		Year:    1984,
		Aliases: []string{"Macintosh (classic)", "Classic Mac", "Mac OS Classic", "Classic Mac OS", "Macintosh 128K", "Mac 68k", "Macintosh Plus", "Mac OS 9", "System 7", "Macintosh Classic"},
		Short:   "MAC",
	},
	{
		Name:    "Apple II",
		Maker:   "Apple",
		Kind:    SystemKindComputer,
		Year:    1977,
		Aliases: []string{"Apple ][", "Apple 2", "Apple II+", "Apple IIe", "Apple IIc", "Apple IIGS", "Apple IIe Platinum", "Apple 2e", "Apple ][+"},
		Short:   "A2",
	},
	// Blaze.
	{
		Name:    "Evercade",
		Maker:   "Blaze",
		Kind:    SystemKindHandheld,
		Year:    2020,
		Aliases: []string{"Evercade VS", "Evercade EXP", "Blaze Evercade", "Blaze Evercade VS", "Blaze Evercade EXP", "Evercade VS/EXP", "Evercade Handheld", "Evercade Home Console"},
		Short:   "EVER",
	},
	// Coleco.
	{
		Name:    "ColecoVision",
		Maker:   "Coleco",
		Kind:    SystemKindHome,
		Year:    1982,
		Aliases: []string{"Coleco Vision", "Coleco ColecoVision"},
		Short:   "CV",
	},
	// Commodore.
	{
		Name:    "Amiga CD32",
		Maker:   "Commodore",
		Kind:    SystemKindHome,
		Year:    1993,
		Aliases: []string{"CD32", "Commodore CD32", "Commodore Amiga CD32", "Amiga CD 32"},
		Short:   "CD32",
	},
	{
		Name:    "Commodore CDTV",
		Maker:   "Commodore",
		Kind:    SystemKindHome,
		Year:    1991,
		Aliases: []string{"CDTV", "Amiga CDTV", "Commodore Dynamic Total Vision"},
		Short:   "CDTV",
	},
	{
		Name:    "Amiga",
		Maker:   "Commodore",
		Kind:    SystemKindComputer,
		Year:    1985,
		Aliases: []string{"Commodore Amiga", "Amiga 500", "Amiga 1200", "Amiga 2000", "Amiga 600", "Amiga 3000", "Amiga 4000", "AmigaOS", "Commodore Amiga 500", "A500", "A1200"},
		Short:   "AMIGA",
	},
	{
		Name:    "Commodore 16 / Plus/4",
		Maker:   "Commodore",
		Kind:    SystemKindComputer,
		Year:    1984,
		Aliases: []string{"Commodore 16", "Commodore Plus/4", "Commodore Plus 4", "Plus/4", "Commodore 116", "C16", "C116", "Plus4", "C+4"},
		Short:   "C16",
	},
	{
		Name:    "Commodore 64",
		Maker:   "Commodore",
		Kind:    SystemKindComputer,
		Year:    1982,
		Aliases: []string{"C64", "Commodore C64", "C=64", "Commodore 64/128", "Commodore C64/128/MAX", "Commodore 128", "C128", "Commodore MAX", "Commodore MAX Machine", "Ultimax", "Commodore 64C", "C64C"},
		Short:   "C64",
	},
	{
		Name:    "VIC-20",
		Maker:   "Commodore",
		Kind:    SystemKindComputer,
		Year:    1980,
		Aliases: []string{"Commodore VIC-20", "VIC 20", "Commodore VIC 20", "VC-20", "VC20", "Commodore VC 20"},
		Short:   "VIC20",
	},
	{
		Name:    "Commodore PET",
		Maker:   "Commodore",
		Kind:    SystemKindComputer,
		Year:    1977,
		Aliases: []string{"Commodore 2001", "Commodore CBM"},
	},
	// Dragon Data.
	{
		Name:    "Dragon 32/64",
		Maker:   "Dragon Data",
		Kind:    SystemKindComputer,
		Year:    1982,
		Aliases: []string{"Dragon 32", "Dragon 64", "Dragon Data Dragon 32", "Dragon Data Dragon 64", "Dragon 200"},
	},
	// Fairchild.
	{
		Name:    "Channel F",
		Maker:   "Fairchild",
		Kind:    SystemKindHome,
		Year:    1976,
		Aliases: []string{"Fairchild Channel F", "Fairchild Video Entertainment System", "Channel F System II", "Fairchild Channel F System II"},
		Short:   "CHF",
	},
	// Fujitsu.
	{
		Name:    "FM Towns",
		Maker:   "Fujitsu",
		Kind:    SystemKindComputer,
		Year:    1989,
		Aliases: []string{"FM Towns Marty", "FM-Towns", "Fujitsu FM Towns", "FM Towns II", "Fujitsu FM Towns Marty", "FM Towns Car Marty"},
		Short:   "FMT",
	},
	{
		Name:    "Fujitsu FM-7",
		Maker:   "Fujitsu",
		Kind:    SystemKindComputer,
		Year:    1982,
		Aliases: []string{"FM-7", "FM7", "FM-77", "Fujitsu FM77", "FM-8", "Fujitsu FM-8", "Fujitsu FM77AV", "FM77AV", "FM-77AV"},
	},
	// GCE.
	{
		Name:    "Vectrex",
		Maker:   "GCE",
		Kind:    SystemKindHome,
		Year:    1982,
		Aliases: []string{"GCE Vectrex", "Milton Bradley Vectrex", "MB Vectrex"},
		Short:   "VEC",
	},
	// MSX.
	{
		Name:    "MSX2",
		Maker:   "MSX",
		Kind:    SystemKindComputer,
		Year:    1985,
		Aliases: []string{"MSX 2", "MSX2+", "MSX 2+", "MSX turboR", "MSX Turbo R"},
	},
	{
		Name:    "MSX",
		Maker:   "MSX",
		Kind:    SystemKindComputer,
		Year:    1983,
		Aliases: []string{"MSX1", "MSX 1", "MSX (1)", "MSX Computer"},
	},
	// Magnavox.
	{
		Name:    "Odyssey 2",
		Maker:   "Magnavox",
		Kind:    SystemKindHome,
		Year:    1978,
		Aliases: []string{"Magnavox Odyssey 2", "Magnavox Odyssey²", "Odyssey²", "Odyssey 2 / Videopac G7000", "Philips Videopac G7000", "Videopac G7000", "Videopac", "Philips Videopac", "Philips Videopac+ G7400", "Videopac+ G7400", "Videopac+", "Philips Odyssey"},
		Short:   "ODY2",
	},
	{
		Name:    "Magnavox Odyssey",
		Maker:   "Magnavox",
		Kind:    SystemKindHome,
		Year:    1972,
		Aliases: []string{"Magnavox Odyssey 1"},
	},
	// Mattel.
	{
		Name:    "HyperScan",
		Maker:   "Mattel",
		Kind:    SystemKindHome,
		Year:    2006,
		Aliases: []string{"Mattel HyperScan"},
	},
	{
		Name:    "Intellivision",
		Maker:   "Mattel",
		Kind:    SystemKindHome,
		Year:    1979,
		Aliases: []string{"Mattel Intellivision", "INTV", "Intellivision II"},
		Short:   "INTV",
	},
	// Nokia.
	{
		Name:    "N-Gage",
		Maker:   "Nokia",
		Kind:    SystemKindHandheld,
		Year:    2003,
		Aliases: []string{"Nokia N-Gage", "N Gage", "N-Gage QD", "Nokia N-Gage QD", "NGage"},
		Short:   "NGAGE",
	},
	// Ouya.
	{
		Name:    "Ouya",
		Maker:   "Ouya",
		Kind:    SystemKindHome,
		Year:    2013,
		Aliases: []string{"Ouya Console"},
		Short:   "OUYA",
	},
	// Panasonic.
	{
		Name:    "3DO",
		Maker:   "Panasonic",
		Kind:    SystemKindHome,
		Year:    1993,
		Aliases: []string{"3DO Interactive Multiplayer", "Panasonic 3DO", "3DO Multiplayer", "Goldstar 3DO", "Panasonic REAL 3DO", "REAL 3DO"},
	},
	// Panic.
	{
		Name:    "Playdate",
		Maker:   "Panic",
		Kind:    SystemKindHandheld,
		Year:    2022,
		Aliases: []string{"Panic Playdate", "Playdate Console"},
		Short:   "PDATE",
	},
	// Philips.
	{
		Name:    "CD-i",
		Maker:   "Philips",
		Kind:    SystemKindHome,
		Year:    1991,
		Aliases: []string{"Philips CD-i", "Philips CDI", "Compact Disc Interactive", "Magnavox CD-i", "Philips Compact Disc Interactive"},
		Short:   "CDI",
	},
	// Sharp.
	{
		Name:    "Sharp X68000",
		Maker:   "Sharp",
		Kind:    SystemKindComputer,
		Year:    1987,
		Aliases: []string{"X68000", "X68K", "Sharp X68K", "Sharp X68000 ACE", "Sharp X68000 Super", "Sharp X68000 XVI", "Sharp X68000 Compact", "Sharp X68030", "X68030"},
		Short:   "X68K",
	},
	{
		Name:    "Sharp X1",
		Maker:   "Sharp",
		Kind:    SystemKindComputer,
		Year:    1982,
		Aliases: []string{"Sharp X1 Turbo", "X1 Turbo", "Sharp X1turbo", "Sharp X1twin", "X1twin", "Sharp X1C"},
	},
	// Sinclair.
	{
		Name:    "Sinclair QL",
		Maker:   "Sinclair",
		Kind:    SystemKindComputer,
		Year:    1984,
		Aliases: []string{"ICL One Per Desk", "One Per Desk", "Merlin Tonto", "Sinclair QL Computer"},
	},
	{
		Name:    "ZX Spectrum",
		Maker:   "Sinclair",
		Kind:    SystemKindComputer,
		Year:    1982,
		Aliases: []string{"Sinclair ZX Spectrum", "ZX Spectrum+", "ZX Spectrum 48K", "ZX Spectrum 128K", "ZX Spectrum +2", "ZX Spectrum +3", "Timex Sinclair 2068", "Timex TS2068"},
		Short:   "ZX",
	},
	{
		Name:    "ZX81",
		Maker:   "Sinclair",
		Kind:    SystemKindComputer,
		Year:    1981,
		Aliases: []string{"Sinclair ZX81", "Timex Sinclair 1000", "Timex Sinclair 1500", "ZX 81"},
	},
	// Tiger.
	{
		Name:    "Game.com",
		Maker:   "Tiger",
		Kind:    SystemKindHandheld,
		Year:    1997,
		Aliases: []string{"Game com", "Tiger Game.com", "Tiger Game com", "Gamecom"},
		Short:   "GCOM",
	},
	{
		Name:    "R-Zone",
		Maker:   "Tiger",
		Kind:    SystemKindHandheld,
		Year:    1995,
		Aliases: []string{"Tiger R-Zone", "RZone", "R Zone"},
	},
	// Other.
	{
		Name:    "V.Smile",
		Maker:   "Other",
		Kind:    SystemKindHome,
		Year:    2004,
		Aliases: []string{"VSmile", "V Smile", "VTech V.Smile", "VTech V Smile", "V.Smile Pocket", "V.Smile Motion"},
	},
	{
		Name:    "Nuon",
		Maker:   "Other",
		Kind:    SystemKindHome,
		Year:    2000,
		Aliases: []string{"VM Labs NUON", "VM Labs Nuon", "Samsung Nuon"},
	},
	{
		Name:    "Casio Loopy",
		Maker:   "Other",
		Kind:    SystemKindHome,
		Year:    1995,
		Aliases: []string{"Loopy", "Casio Loopy My Seal Computer", "Loopy My Seal Computer"},
	},
	{
		Name:    "LaserActive",
		Maker:   "Other",
		Kind:    SystemKindHome,
		Year:    1993,
		Aliases: []string{"Pioneer LaserActive", "LaserActive (Pioneer)", "Pioneer LaserActive LD-ROM", "Mega LD", "LaserActive Mega LD", "LaserActive PC Engine Pack", "LD-ROM2"},
	},
	{
		Name:    "Super Cassette Vision",
		Maker:   "Other",
		Kind:    SystemKindHome,
		Year:    1984,
		Aliases: []string{"Epoch Super Cassette Vision", "SCV", "Yeno Super Cassette Vision", "Yeno SCV"},
	},
	{
		Name:    "Emerson Arcadia 2001",
		Maker:   "Other",
		Kind:    SystemKindHome,
		Year:    1982,
		Aliases: []string{"Arcadia 2001"},
	},
	{
		Name:    "Epoch Cassette Vision",
		Maker:   "Other",
		Kind:    SystemKindHome,
		Year:    1981,
		Aliases: []string{"Cassette Vision"},
	},
	{
		Name:    "Interton VC 4000",
		Maker:   "Other",
		Kind:    SystemKindHome,
		Year:    1978,
		Aliases: []string{"VC 4000", "VC4000", "Interton VC4000", "Interton Video Computer 4000", "Interton Video 2000"},
	},
	{
		Name:    "Bally Astrocade",
		Maker:   "Other",
		Kind:    SystemKindHome,
		Year:    1977,
		Aliases: []string{"Astrocade", "Bally Arcade", "Bally Professional Arcade", "Astrovision Professional Arcade", "Bally Professional Arcade (Astrocade)"},
	},
	{
		Name:    "Gizmondo",
		Maker:   "Other",
		Kind:    SystemKindHandheld,
		Year:    2005,
		Aliases: []string{},
	},
	{
		Name:    "Tapwave Zodiac",
		Maker:   "Other",
		Kind:    SystemKindHandheld,
		Year:    2003,
		Aliases: []string{"Tapwave Zodiac 1", "Tapwave Zodiac 2"},
	},
	{
		Name:    "Mega Duck",
		Maker:   "Other",
		Kind:    SystemKindHandheld,
		Year:    1993,
		Aliases: []string{"Cougar Boy", "Mega Duck / Cougar Boy", "Mega Duck/Cougar Boy"},
	},
	{
		Name:    "Watara Supervision",
		Maker:   "Other",
		Kind:    SystemKindHandheld,
		Year:    1992,
		Aliases: []string{"Watara/QuickShot Supervision", "QuickShot Supervision", "Watara QuickShot Supervision", "Watara Super Vision"},
	},
	{
		Name:    "Gamate",
		Maker:   "Other",
		Kind:    SystemKindHandheld,
		Year:    1990,
		Aliases: []string{"Bit Corporation Gamate", "Bit Corp Gamate"},
	},
	{
		Name:    "Microvision",
		Maker:   "Other",
		Kind:    SystemKindHandheld,
		Year:    1979,
		Aliases: []string{"Milton Bradley Microvision", "MB Microvision"},
	},
	{
		Name:    "Thomson MO5",
		Maker:   "Other",
		Kind:    SystemKindComputer,
		Year:    1984,
		Aliases: []string{"MO5", "Thomson M05", "Thomson MO5E", "Thomson TO7", "Thomson TO7/70", "Thomson TO8", "Thomson TO9", "Thomson MO6"},
	},
	{
		Name:    "Texas Instruments TI-99/4A",
		Maker:   "Other",
		Kind:    SystemKindComputer,
		Year:    1981,
		Aliases: []string{"TI-99/4A", "TI-99", "TI 99/4A", "TI-99/4", "Texas Instruments TI-99", "TI99", "TI-99/4A Home Computer"},
	},
	{
		Name:    "TRS-80 Color Computer",
		Maker:   "Other",
		Kind:    SystemKindComputer,
		Year:    1980,
		Aliases: []string{"TRS-80 CoCo", "CoCo", "Tandy Color Computer", "Tandy CoCo", "Radio Shack Color Computer", "TRS-80 Color Computer 2", "TRS-80 Color Computer 3", "Color Computer 3", "CoCo 3"},
	},
}

// systemIndex maps the key of every name and alias to its system's position in catalog.
var systemIndex = func() map[string]int {
	out := map[string]int{}

	for i, s := range catalog {
		for _, w := range append([]string{s.Name}, s.Aliases...) {
			out[SystemKey(w)] = i
		}
	}

	return out
}()

// Systems returns the catalog in display order (a copy: callers cannot change it).
func Systems() []System {
	out := make([]System, len(catalog))

	for i, s := range catalog {
		s.Aliases = slices.Clone(s.Aliases)
		out[i] = s
	}

	return out
}

// SystemKey is how names and aliases are compared: lower case, without spaces, hyphens and dots
// ("P.S. 4" and "ps-4" are "ps4"). The web's systemKey does the same (web/tests/systemKey.cases.json).
func SystemKey(s string) string {
	var b strings.Builder

	for _, r := range strings.ToLower(s) {
		if unicode.IsSpace(r) || r == '-' || r == '.' {
			continue
		}

		b.WriteRune(r)
	}

	return b.String()
}

// SystemNamed returns the catalog system s names, by its name or one of its aliases.
func SystemNamed(s string) (System, bool) {
	k := SystemKey(s)
	if k == "" {
		return System{}, false
	}

	i, ok := systemIndex[k]
	if !ok {
		return System{}, false
	}

	return Systems()[i], true
}

// IsModernSystem reports whether name is a catalog system with Modern set.
func IsModernSystem(name string) bool {
	i := slices.IndexFunc(catalog, func(s System) bool { return s.Name == name })

	return i >= 0 && catalog[i].Modern
}
