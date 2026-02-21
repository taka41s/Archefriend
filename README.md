# ArcheFriend v3.7

Overlay and automation tool for ArcheAge. Windows only, requires Administrator privileges.

## Requirements

- **Go 1.21+**
- **Windows 10/11**
- **Run as Administrator** (required for memory read/write)
- **ArcheAge running** (`archeage.exe` + `x2game.dll`)

## Build

```bat
build.bat
```

Interactive menu:
- **[A]** All features (everything enabled)
- **[C]** Custom (choose features)
- **[M]** Minimal (overlay + patches only)

Manual build:
```bash
go build -ldflags "-s -w" -o archefriend.exe main.go
```

Disable features at compile time:
```bash
go build -ldflags "-s -w -X main.featureESP=false -X main.featureBot=false" -o archefriend.exe main.go
```

---

## Features

### Loot/Doodad Bypass (`featureLoot`)
Patches loot distance checks and doodad interaction checks.

| Key | Action |
|-----|--------|
| **F1** | Toggle Loot Bypass |
| **F2** | Toggle Doodad Bypass |

### Patches (`featurePatches`)
Applied automatically on startup, restored on exit:
- Mount bypass (mount anywhere)
- GCD removal (removes global cooldown)

### Reactions (`featureReactions`)
Monitors player and target buffs/debuffs in real time (~1ms). When a configured buff/debuff is detected, presses keys automatically.

| Key | Action |
|-----|--------|
| **F5** | Reload `reactions.json` |
| **F6** | Toggle Reactions ON/OFF |
| **F7** | Open Reactions Config window |

Features:
- Reacts to **player** or **target** buffs/debuffs
- Auto-pause when AFK (10s without input)
- Configurable cooldown per reaction
- Key spam (5x) to ensure registration

### Buff Injector (`featureBuffs`)
Injects buffs directly into game memory.

| Key | Action |
|-----|--------|
| **F8** | Open Buffs window |
| **F9** | Toggle Quick Buff Preset |

Features:
- Inject buff with any ID
- Permanent buff (freeze loop keeps it active)
- Hidden buff (doesn't appear in game UI)
- Buff presets (groups saved in `buff_presets.json`)

### ESP (`featureESP`)
Transparent overlay drawn over the game using GDI.

| Key | Action |
|-----|--------|
| **F12** | Toggle Target ESP |
| **-** (minus) | Toggle All Entities ESP |
| **=** (equals) | Toggle Show Players |
| **[** | Toggle Show NPCs |
| **]** | Toggle House ESP |
| **'** | Next house filter |
| **HOME** | Cycle ESP style |
| **PAGE UP** | Toggle Recheck Panel |
| **SCROLL LOCK** | Start/Stop Target Scanner |
| **PAUSE** | Trigger manual scan |

Features:
- Target ESP (name, HP, distance)
- All entities ESP (players, NPCs, mobs)
- House ESP (owner, tax, protection, demolition)
- Aimbot (moves cursor to target)
- Lua export for addons (via `settings.json`)

### Bot (`featureBot`)
Combat bot. Requires active ESP.

| Key | Action |
|-----|--------|
| **DELETE** or **NUMPAD0** | Toggle Bot ON/OFF |
| **NUMPAD1** | Load Preset 1 |
| **NUMPAD2** | Load Preset 2 |
| **NUMPAD3** | Load Preset 3 |
| **NUMPAD4** | Reload `bot_config.json` |
| **NUMPAD5** | Toggle Partial Match |
| **NUMPAD+** | Increase range +5m |
| **NUMPAD-** | Decrease range -5m |
| **NUMPAD9** | Print bot stats |
| **F10** | Open Bot Config window |

Features:
- Auto-target by name (exact or partial)
- Auto-attack with configurable key
- Auto-loot after kill
- Auto-potion (HP/MP) with threshold and cooldown
- Mob presets for quick switching

### Fishing Bot
Sport fishing bot. Monitors target buffs and presses the corresponding keys.

| Key | Action |
|-----|--------|
| **F11** | Open Fishing Bot window |

Default buffs:
| Buff ID | Action | Default Key |
|---------|--------|-------------|
| 5264 | RIGHT | D |
| 5265 | LEFT | A |
| 5508 | BIG REEL IN | SPACE |
| 5266 | SMALL REEL IN | W |
| 5267 | ESCAPE | S |

### Skill Reactions
Hooks into the game process to detect skill casts. Automatically executes keys in reaction.

| Key | Action |
|-----|--------|
| **PAGE DOWN** | Open Skill Reactions window |

Requires offsets in `settings.json`:
```json
{
  "skill_cast_offset": "0x1A2B3C",
  "skill_try_offset": "0x1A2B3D"
}
```

Features:
- Detects skill cast by ID
- Detects cast attempt (pre-validation)
- Automatic aimbot before/during cast
- Configurable key spam
- Buff condition (only executes if buff is present/absent)

### KeySpam (`featureKeyspam`)
Sends keys to the game window via PostMessage.

| Key | Action |
|-----|--------|
| **F3** | Send configured keys (1x) |
| **F4** | Toggle Auto-Spam |
| **INSERT** | Open AutoSpam Config window |

### Other Keys

| Key | Action |
|-----|--------|
| **END** | Show/hide overlay |
| **NUMPAD8** | Diagnostics (full dump to console) |

---

## Configuration Files

All JSON files must be in the same folder as the `.exe`.

### `settings.json`
```json
{
  "lua_export_path": "C:\\path\\to\\scan.lua",
  "skill_cast_offset": "0x1A2B3C",
  "skill_try_offset": "0x1A2B3D"
}
```

### `reactions.json`
```json
[
  {
    "type": 87,
    "name": "Hell Spear",
    "onStart": "F10",
    "onEnd": "",
    "isDebuff": true,
    "cooldownMs": 1000,
    "source": "player"
  }
]
```

- `type`: Buff/debuff ID
- `onStart`: Keys on gain (`ALT+Q`, `SHIFT+5 & R`)
- `onEnd`: Keys on loss
- `source`: `"player"` or `"target"`

### `bot_config.json`
```json
{
  "mob_names": ["Bluescale Archerfish"],
  "max_range": 25,
  "partial_match": false,
  "scan_interval_ms": 20,
  "target_delay_ms": 50,
  "attack_key": "E+3+4",
  "loot_key": "SHIFT+F",
  "attack_delay": 500,
  "loot_delay": 300,
  "auto_attack": false,
  "auto_loot": false,
  "hp_potion_key": "F1",
  "hp_potion_threshold": 99,
  "hp_potion_enabled": false,
  "mp_potion_key": "F2",
  "mp_potion_threshold": 99,
  "mp_potion_enabled": false,
  "potion_cooldown_ms": 21000,
  "presets": {
    "preset1": ["Bluescale Archerfish"],
    "preset2": ["Wandering Imp"],
    "preset3": []
  }
}
```

### `fishing_config.json`
```json
{
  "actions": [
    { "buffId": 5264, "name": "RIGHT", "keybind": "D" },
    { "buffId": 5265, "name": "LEFT", "keybind": "A" },
    { "buffId": 5508, "name": "BIG REEL IN", "keybind": "SPACE" },
    { "buffId": 5266, "name": "SMALL REEL IN", "keybind": "W" },
    { "buffId": 5267, "name": "ESCAPE", "keybind": "S" }
  ]
}
```

### `skill_reactions.json`
```json
{
  "reactions": [
    {
      "skillId": 10005,
      "name": "Fireball",
      "onCast": "ALT+Q",
      "enabled": true,
      "cooldownMs": 500,
      "useAimbot": false,
      "aimbotOnTry": false,
      "spamCount": 1,
      "requireBuffId": 0,
      "requireBuffAbsent": false
    }
  ]
}
```

### `buff_presets.json`
```json
[
  {
    "name": "Movement Buffs",
    "description": "Movement speed buffs",
    "buffs": [
      { "id": 5909, "name": "Swim", "permanent": true, "hidden": false, "stack": 0 }
    ]
  }
]
```

### `aimbot_config.json`
```json
{
  "enabled": true,
  "keys": [
    { "name": "Mouse4", "code": 5 },
    { "name": "Mouse5", "code": 6 }
  ]
}
```

---

## Key Syntax

Used in reactions, bot, fishing, and skill reactions:

- Single key: `A`, `F1`, `SPACE`, `TAB`
- Modifier combo: `ALT+Q`, `SHIFT+F`, `CTRL+1`
- Sequence: `SHIFT+5 & R` (presses SHIFT+5, then R)
- Multiple keys: `E+3+4` (presses E, 3, and 4 in sequence)

Special keys: `LSHIFT`, `RSHIFT`, `LCTRL`, `RCTRL`, `LALT`, `RALT`, `SPACE`, `TAB`, `ENTER`, `ESC`, `BACKSPACE`, `DELETE`, `INSERT`, `HOME`, `END`, `PAGEUP`, `PAGEDOWN`, `UP`, `DOWN`, `LEFT`, `RIGHT`, `F1`-`F12`, `NUMPAD0`-`NUMPAD9`

---

## Feature Flags

| Flag | Default | Controls |
|------|---------|----------|
| `featureLoot` | `true` | Loot/Doodad bypass |
| `featurePatches` | `true` | Mount/GCD patches |
| `featureReactions` | `true` | Buff reactions + AFK + Fishing bot |
| `featureBuffs` | `true` | Buff injector + presets |
| `featureESP` | `true` | ESP + Aimbot + House scanner |
| `featureBot` | `true` | Combat bot |
| `featureKeyspam` | `true` | Key spam / auto-spam |
