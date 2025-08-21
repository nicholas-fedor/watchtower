package data

// containerNames is an array of realistic container names used in the preview tool.
// These are used to simulate container names in the notification output, matching the style of real Watchtower containers.
var containerNames = []string{
	"cyberscribe",
	"datamatrix",
	"nexasync",
	"quantumquill",
	"aerosphere",
	"virtuos",
	"fusionflow",
	"neuralink",
	"pixelpulse",
	"synthwave",
	"codecraft",
	"zapzone",
	"robologic",
	"dreamstream",
	"infinisync",
	"megamesh",
	"novalink",
	"xenogenius",
	"ecosim",
	"innovault",
	"techtracer",
	"fusionforge",
	"quantumquest",
	"neuronest",
	"codefusion",
	"datadyno",
	"pixelpioneer",
	"vortexvision",
	"cybercraft",
	"synthsphere",
	"infinitescript",
	"roborhythm",
	"dreamengine",
	"aquasync",
	"geniusgrid",
	"megamind",
	"novasync-pro",
	"xenonwave",
	"ecologic",
	"innoscan",
}

// organizationNames is an array of realistic organization names used to construct image names in the preview tool.
// These prepend container names to form realistic image references (e.g., "techwave/cyberscribe:latest").
var organizationNames = []string{
	"techwave",
	"codecrafters",
	"innotechlabs",
	"fusionsoft",
	"cyberpulse",
	"quantumscribe",
	"datadynamo",
	"neuralink",
	"pixelpro",
	"synthwizards",
	"virtucorplabs",
	"robologic",
	"dreamstream",
	"novanest",
	"megamind",
	"xenonwave",
	"ecologic",
	"innosync",
	"techgenius",
	"nexasoft",
	"codewave",
	"zapzone",
	"techsphere",
	"aquatech",
	"quantumcraft",
	"neuronest",
	"datafusion",
	"pixelpioneer",
	"synthsphere",
	"infinitescribe",
	"roborhythm",
	"dreamengine",
	"vortexvision",
	"geniusgrid",
	"megamesh",
	"novasync",
	"xenogeniuslabs",
	"ecosim",
	"innovault",
}

// infoMessages is an array of realistic info-level log messages based on typical Watchtower operations.
// These are derived from real Watchtower logs, including container operations, session management, and notifications.
var infoMessages = []string{
	"Detected multiple Watchtower instances, initiating cleanup",
	"Stopping container",
	"Removing image",
	"Successfully cleaned up excess Watchtower instances",
	"Watchtower v1.11.7 using Docker API v1.51",
	"Using notifications: gotify",
	"Checking all containers (except explicitly disabled with label)",
	"Scheduling first run: 2025-08-19 06:00:00 +0000 UTC",
	"Note that the first check will be performed in 5 hours, 59 minutes, 22 seconds",
	"Update session completed",
	"Found new image",
	"Started new container",
}

// warningMessages is an array of realistic warning-level messages based on potential Watchtower issues.
// These reflect non-critical issues that may occur during operations, such as failures in specific tasks.
var warningMessages = []string{
	"Failed to stop container",
	"Failed to remove image",
	"Failed to start new container",
	"Update session failed",
	"Failed to find new image",
	"Failed to list containers",
	"Failed to inspect container",
}

// errorMessages is an array of realistic error-level messages based on critical Watchtower failures.
// These include issues like update failures or network errors, derived from real logs and error cases.
var errorMessages = []string{
	"Update installation failed. Rolling back to the previous version...",
	"Unable to check for updates. Please check your internet connection.",
	"Update verification failed. Please contact support.",
	"Update package download failed. Try again later.",
	"Failed to stop container",
	"Failed to remove image",
	"Failed to start new container",
	"Update session failed",
	"Failed to find new image",
	"Failed to list containers",
	"Failed to inspect container",
}

// skippedMessages is an array of realistic error messages for containers in the SkippedState.
// These reflect reasons why containers might be skipped during updates, based on typical user scenarios.
var skippedMessages = []string{
	"container skipped: Avoiding potential subscription fees",
	"container skipped: Concerns about update breaking third-party plugins or extensions",
	"container skipped: Avoiding potential changes in licensing terms",
	"container skipped: Concerns about compatibility with other software",
	"container skipped: Fear of losing access to older file formats",
	"container skipped: Prefer the older version's features or design",
	"container skipped: Worries about losing custom settings or configurations",
	"container skipped: Lack of trust in the software developer's updates",
	"container skipped: Don't want to relearn how to use the software",
	"container skipped: Current version works fine for my needs",
	"container skipped: Fear of introducing new bugs",
	"container skipped: Limited bandwidth for downloading updates",
}

// Note: These arrays can be expanded with additional messages from real logs to increase variety.
// The preview tool randomly selects from these to simulate dynamic output.
// Levels are separated to allow precise mapping in `logs.go` (e.g., error from errorMessages, warning from warningMessages, info from infoMessages).
