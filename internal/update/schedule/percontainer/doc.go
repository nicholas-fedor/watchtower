// Package percontainer gives containers their own update schedules.
//
// A container labeled com.centurylinklabs.watchtower.schedule (cron syntax, as
// --schedule) is updated on that schedule instead of the global one. The
// scheduler keeps one cron entry for each distinct schedule in use.
//
// The parts are kept apart so each can change on its own:
//
//   - Which containers a run covers is decided when the run starts, from the
//     label itself ([OnSchedule], [WithoutOwnSchedule]). Nothing is cached about
//     a container's membership.
//   - [Registry] keeps the set of schedules in use, as a count of containers per
//     schedule, and adds or removes cron entries ([Timers]) as the set changes.
//   - A [Source] tells the registry which containers carry which schedule. It is
//     the only part that talks to the container runtime. [DockerSource] reads
//     Docker's container events, so nothing is polled: it lists the containers
//     once when it connects, then follows creates, destroys and renames.
package percontainer
