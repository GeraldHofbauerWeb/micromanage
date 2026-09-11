// Package proc prepares child processes the launcher starts in the
// background: Java probes, loader installers, the game itself.
//
// The Windows GUI is linked as a GUI application, so it has no console of its
// own. Any console program it starts would then get a fresh console window of
// its own, flashing up behind the launcher. Every child here has its output
// piped back anyway, so the window is never needed.
package proc
