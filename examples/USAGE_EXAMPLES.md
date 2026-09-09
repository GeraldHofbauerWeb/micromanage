# Usage Examples

This document provides practical examples for common use cases of MicroManage.

## 🎮 Gaming Scenarios

### Multiple Modpacks

```bash
# Set up different modpacks
micromanage create skyfactory
micromanage create stoneblock
micromanage create enigmatica

# Add mods to each
cp SkyFactory-mods/* ~/.minecraft-instances/skyfactory/mods/
cp StoneBlock-mods/* ~/.minecraft-instances/stoneblock/mods/
cp Enigmatica-mods/* ~/.minecraft-instances/enigmatica/mods/

# Switch between them
micromanage switch skyfactory
# Play Sky Factory...
micromanage switch stoneblock
# Play Stone Block...
```

### Minecraft Versions

```bash
# Different Minecraft versions
micromanage create mc-1.19.4-forge
micromanage create mc-1.20.1-forge
micromanage create mc-1.21-neoforge

# Switch based on what you want to play
micromanage switch mc-1.20.1-forge
```

## 🔧 Development Scenarios

### Mod Development Workflow

```bash
# Create development instances
micromanage create clean-testing      # No other mods
micromanage create compatibility-test # With common mods
micromanage create performance-test   # With performance mods

# Development cycle
micromanage switch clean-testing
# Test your mod in isolation

micromanage switch compatibility-test  
# Test with other popular mods

micromanage switch performance-test
# Check performance impact
```

### Version Testing

```bash
# Test your mod across Minecraft versions
micromanage create dev-1.20.1
micromanage create dev-1.20.4
micromanage create dev-1.21

# Add your mod to each and test
cp my-mod-1.20.1.jar ~/.minecraft-instances/dev-1.20.1/mods/
cp my-mod-1.20.4.jar ~/.minecraft-instances/dev-1.20.4/mods/
cp my-mod-1.21.jar ~/.minecraft-instances/dev-1.21/mods/
```

## 📦 Modpack Creation

### Building a Modpack

```bash
# Create base modpack
micromanage create my-modpack-base
micromanage switch my-modpack-base

# Add mods incrementally and test
cp essential-mods/* ~/.minecraft/mods/
# Test stability...

cp optional-mods/* ~/.minecraft/mods/
# Test compatibility...

# Create variants
micromanage create my-modpack-lite
micromanage create my-modpack-full

# Distribute the lite version
tar -czf my-modpack-lite.tar.gz ~/.minecraft-instances/my-modpack-lite/
```

### A/B Testing

```bash
# Compare configurations
micromanage create config-a
micromanage create config-b

# Test different mod configurations
micromanage switch config-a
# Configure mods one way...

micromanage switch config-b  
# Configure mods differently...

# Compare performance/stability
```

## 🚀 Advanced Workflows

### Backup Strategy

```bash
# Before major changes, create backup
micromanage create modpack-backup-$(date +%Y%m%d)

# Copy current instance  
cp -r ~/.minecraft-instances/my-modpack ~/.minecraft-instances/modpack-backup-$(date +%Y%m%d)/

# Make changes safely
micromanage switch my-modpack
# Add experimental mods...

# If issues occur, restore backup
micromanage switch modpack-backup-$(date +%Y%m%d)
```

### Sharing with Friends

```bash
# Prepare instance for sharing
micromanage create friend-modpack
micromanage switch friend-modpack

# Add mods and configure
# Clean up personal data (remove saves, etc.)
rm -rf ~/.minecraft/saves/*

# Package for sharing
cd ~/.minecraft-instances/
tar -czf friend-modpack.tar.gz friend-modpack/

# Send friend-modpack.tar.gz to friends
# They extract to ~/.minecraft-instances/ and switch to it
```

### Server Sync

```bash
# Sync with server modpack
micromanage create server-sync
micromanage switch server-sync

# Download server mods
wget server.com/modpack-mods.zip
unzip modpack-mods.zip -d ~/.minecraft/mods/

# Keep in sync
micromanage switch server-sync
# Update mods as server updates...
```

## 🛠️ Maintenance

### Cleaning Up

```bash
# List all instances to see what you have
micromanage list

# Switch to temporary instance before cleanup
micromanage switch vanilla

# Remove unused instances
rm -rf ~/.minecraft-instances/old-instance-name

# Restore if needed
micromanage restore
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
# See mod counts
micromanage list

# Check current instance
micromanage list | grep "Current instance"
```

### Scripted Workflows
```bash
#!/bin/bash
# dev-cycle.sh - Development workflow

echo "Building mod..."
./gradlew build

echo "Updating test instance..."
cp build/libs/*.jar ~/.minecraft-instances/dev-test/mods/

echo "Switching to test instance..."
micromanage switch dev-test

echo "Ready for testing!"
```

### Safe Experimentation
```bash
# Always work on copies when experimenting
cp -r ~/.minecraft-instances/stable ~/.minecraft-instances/experimental
micromanage switch experimental
# Experiment safely...

# Restore stable if needed
micromanage switch stable
```

---

**Remember**: The instance manager uses symlinks, so switching is instant and safe. Always keep backups of important configurations!