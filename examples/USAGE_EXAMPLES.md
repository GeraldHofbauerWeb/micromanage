# Usage Examples

This document provides practical examples for common use cases of Instant Launcher.

Every instance is a folder of its own under the instances directory
(`instant-mc config instances-path` shows where), and the game runs in it
directly. There is nothing to switch: launch the instance you want. The
official launcher's `.minecraft` is only ever read, to import it.

## 🎮 Gaming Scenarios

### Bringing your existing Minecraft along

```bash
# Copy the official launcher's .minecraft into an instance called Default
instant-mc import

# Or under another name, without the worlds
instant-mc import vanilla-survival --no-saves

# Play it
instant-mc launch Default
```

### Multiple Modpacks

```bash
# Set up different modpacks
instant-mc create skyfactory
instant-mc create stoneblock
instant-mc create enigmatica

# Add mods to each
cp SkyFactory-mods/* ~/.minecraft-instances/skyfactory/mods/
cp StoneBlock-mods/* ~/.minecraft-instances/stoneblock/mods/
cp Enigmatica-mods/* ~/.minecraft-instances/enigmatica/mods/

# Play whichever you like
instant-mc launch skyfactory
# Play Sky Factory...
instant-mc launch stoneblock
# Play Stone Block...
```

### Minecraft Versions

```bash
# Different Minecraft versions
instant-mc create mc-1.19.4-forge
instant-mc create mc-1.20.1-forge
instant-mc create mc-1.21-neoforge

# Launch based on what you want to play
instant-mc launch mc-1.20.1-forge
```

## 🔧 Development Scenarios

### Mod Development Workflow

```bash
# Create development instances
instant-mc create clean-testing      # No other mods
instant-mc create compatibility-test # With common mods
instant-mc create performance-test   # With performance mods

# Development cycle
instant-mc launch clean-testing
# Test your mod in isolation

instant-mc launch compatibility-test
# Test with other popular mods

instant-mc launch performance-test
# Check performance impact
```

### Version Testing

```bash
# Test your mod across Minecraft versions
instant-mc create dev-1.20.1
instant-mc create dev-1.20.4
instant-mc create dev-1.21

# Add your mod to each and test
cp my-mod-1.20.1.jar ~/.minecraft-instances/dev-1.20.1/mods/
cp my-mod-1.20.4.jar ~/.minecraft-instances/dev-1.20.4/mods/
cp my-mod-1.21.jar ~/.minecraft-instances/dev-1.21/mods/
```

## 📦 Modpack Creation

### Building a Modpack

```bash
# Create base modpack
instant-mc create my-modpack-base

# Add mods incrementally and test
cp essential-mods/* ~/.minecraft-instances/my-modpack-base/mods/
instant-mc launch my-modpack-base
# Test stability...

cp optional-mods/* ~/.minecraft-instances/my-modpack-base/mods/
instant-mc launch my-modpack-base
# Test compatibility...

# Create variants from it
instant-mc create my-modpack-lite --clone my-modpack-base
instant-mc create my-modpack-full --clone my-modpack-base

# Distribute the lite version
tar -czf my-modpack-lite.tar.gz -C ~/.minecraft-instances my-modpack-lite
```

### A/B Testing

```bash
# Compare configurations
instant-mc create config-a
instant-mc create config-b

# Test different mod configurations
instant-mc launch config-a
# Configure mods one way...

instant-mc launch config-b
# Configure mods differently...

# Compare performance/stability
```

## 🚀 Advanced Workflows

### Backup Strategy

```bash
# Before major changes, make a copy (worlds included)
instant-mc create modpack-backup-$(date +%Y%m%d) --clone my-modpack --with-saves --with-screenshots

# Make changes safely
instant-mc launch my-modpack
# Add experimental mods...

# If issues occur, play the backup
instant-mc launch modpack-backup-$(date +%Y%m%d)
```

### Sharing with Friends

```bash
# Prepare instance for sharing
instant-mc create friend-modpack
# Add mods and configure; worlds stay out of a clone unless asked for

# Package for sharing
tar -czf friend-modpack.tar.gz -C ~/.minecraft-instances friend-modpack

# Send friend-modpack.tar.gz to friends
# They extract it into their instances directory and launch it
```

### Server Sync

```bash
# Sync with server modpack
instant-mc create server-sync

# Download server mods
wget server.com/modpack-mods.zip
unzip modpack-mods.zip -d ~/.minecraft-instances/server-sync/mods/

# Play
instant-mc launch server-sync --server play.example.com
```

## 🛠️ Maintenance

### Cleaning Up

```bash
# List all instances to see what you have
instant-mc list

# Remove unused instances (asks first)
instant-mc delete old-instance-name
```

### Regular Backups

```bash
#!/bin/bash
# backup-instances.sh - Run weekly

DATE=$(date +%Y%m%d)
BACKUP_DIR="$HOME/minecraft-backups/$DATE"

mkdir -p "$BACKUP_DIR"
cp -r ~/.minecraft-instances "$BACKUP_DIR/"

# Keep only last 4 backups
ls -t ~/minecraft-backups/ | tail -n +5 | xargs -d '\n' -r rm -rf --
```

## 💡 Tips & Tricks

### Quick Instance Info
```bash
# See mod counts, and which instance was used last
instant-mc list
```

### Scripted Workflows
```bash
#!/bin/bash
# dev-cycle.sh - Development workflow

echo "Building mod..."
./gradlew build

echo "Updating test instance..."
cp build/libs/*.jar ~/.minecraft-instances/dev-test/mods/

echo "Launching the test instance..."
instant-mc launch dev-test --wait
```

### Safe Experimentation
```bash
# Always work on copies when experimenting
instant-mc create experimental --clone stable --with-saves
instant-mc launch experimental
# Experiment safely...

# The stable one is untouched
instant-mc launch stable
```

---

**Remember**: instances sit side by side and never touch each other or the
official launcher's `.minecraft`. Always keep backups of important
configurations!
