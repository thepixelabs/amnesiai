package tui

import (
	"hash/fnv"
	"time"
)

// Greeting holds an icon and a quip for the time-of-day greeting system.
type Greeting struct {
	Icon string
	Text string
}

var greetingWindows = []struct {
	id    string
	start int
	end   int
}{
	{"dead_of_night", 0, 2},
	{"late_night", 3, 5},
	{"early_morning", 6, 8},
	{"morning", 9, 11},
	{"midday", 12, 13},
	{"afternoon", 14, 16},
	{"evening", 17, 19},
	{"night", 20, 23},
}

// greetingBank is keyed by window id with developer-themed quips.
// Same structure and hour windows as altergo_greetings.py.
var greetingBank = map[string][]Greeting{
	"dead_of_night": {
		{"🌑", "Midnight configs still need saving."},
		{"💾", "Past midnight. Your dotfiles are not tired."},
		{"🔐", "Dark hours call for encrypted backups."},
		{"📜", "The git log ends here. The backup does not."},
		{"🤖", "The backup daemon has no opinion on your sleep schedule."},
	},
	"late_night": {
		{"❌", "Three AM backups — bold move."},
		{"😴", "CI is asleep. Your configs are not."},
		{"⚠️", "Nothing good was ever committed at this hour."},
		{"💀", "Late enough that the restore test matters even more."},
		{"🌀", "Deep night, deep backup. Respect."},
	},
	"early_morning": {
		{"⚡", "First snapshot of the day. The record is clean."},
		{"☕", "The coffee hasn't lied to you yet today."},
		{"🐦", "Dawn and a fresh backup — similarly refreshing."},
		{"✅", "Early start. Your configs thank you."},
		{"🌅", "Up before the sun. Making sure configs survive the day."},
	},
	"morning": {
		{"📌", "Morning. Good time for a backup."},
		{"☕", "Two cups of coffee from full productivity. Configs: ready."},
		{"📆", "The workday has opinions. Your dotfiles do not."},
		{"🔄", "Fresh session. Fresh snapshot."},
		{"🏃", "The day is young. Back it up while it's clean."},
	},
	"midday": {
		{"😅", "Noon. The morning got away from you. The config didn't."},
		{"💡", "Pre-lunch backup: use it before context evaporates."},
		{"🧠", "Late enough to have changed something, early enough to save it."},
		{"⚖️", "Halfway through the day. Half your configs are safe."},
		{"🎬", "The morning was a rehearsal. This backup is real."},
	},
	"afternoon": {
		{"🌫️", "Post-lunch fog is optional. The backup is not."},
		{"📝", "The afternoon is long. So is your config list."},
		{"🔬", "The feature exists in theory. The backup will verify."},
		{"🦆", "Somewhere a rubber duck is solving someone's config problem."},
		{"☕", "Three PM. The caffeine wore off. The backup remains."},
	},
	"evening": {
		{"🌙", "After hours. Your configs are still on the clock."},
		{"🚢", "Ship it or stash it — same question for configs."},
		{"✅", "The build passed. Back it up before it stops."},
		{"📖", "Whatever didn't ship today, at least it's saved."},
		{"🎯", "Evening: the line between work and hobby blurs. Backup anyway."},
	},
	"night": {
		{"🤡", "A reasonable time to start a config audit. Said no one."},
		{"🔥", "You and the dotfiles, alone again. This is fine."},
		{"⌨️", "The keyboard has been patient with you all day."},
		{"🔮", "Ten PM. The backup is close. It has always been close."},
		{"🌃", "Night shift, asked for or not. Configs secured."},
	},
}

func windowForHour(hour int) string {
	for _, w := range greetingWindows {
		if hour >= w.start && hour <= w.end {
			return w.id
		}
	}
	return "morning"
}

// PickGreeting returns the greeting for the current time, stable per-minute
// (same seeding strategy as altergo_greetings.py: seed = int(time.time()//60)).
func PickGreeting() Greeting {
	now := time.Now()
	window := windowForHour(now.Hour())
	bank := greetingBank[window]
	if len(bank) == 0 {
		bank = greetingBank["morning"]
	}
	h := fnv.New32a()
	h.Write([]byte(window))
	seed := int(now.Unix()/60) ^ int(h.Sum32())
	idx := ((seed % len(bank)) + len(bank)) % len(bank)
	return bank[idx]
}
