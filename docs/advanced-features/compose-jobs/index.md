# Compose Jobs

Docker Compose 5.8.0 and later can declare [jobs](https://docs.docker.com/compose/how-tos/jobs/){target="_blank" rel="noopener noreferrer"}.
A job is a container that runs to completion, either on demand or on a cron schedule.
Docker Engine 29.10.0 and later runs jobs through its jobs extension, which is experimental and off by default.
This page explains how Watchtower handles jobs and how to back up your containers before Watchtower updates them.

## How Watchtower Handles Jobs

Docker Engine manages the containers it runs for a job.
It creates a new container for each run and removes it when a scheduled run succeeds.
When a run fails, the engine keeps its container so you can find out what went wrong.
Watchtower leaves all of this to the engine:

- Watchtower never updates or restarts a job's container. It recognizes these containers by the `com.docker.job.id` label and skips them, whatever its [container selection](../../getting-started/container-selection/index.md) settings are. A failed run keeps its exit status and logs even with the [Include Stopped Containers](../../configuration/container-selection/index.md#include_stopped_containers) or [Revive Stopped Containers](../../configuration/container-selection/index.md#revive_stopped_containers) configuration option turned on.
- Watchtower doesn't pull images for jobs. Each run starts from the image that the job's image reference points to on the Docker host at that time. To update a job, pull its image with `docker pull IMAGE`, where *`IMAGE`* is the job's image reference. The next run uses the new image.
- When Watchtower applies a [Git-monitored Compose project](../git-monitoring/index.md#what_watchtower_applies), it updates the services and leaves the jobs out. To register a changed job, follow the steps in Compose's [Change a registered job](https://docs.docker.com/compose/how-tos/jobs/#change-a-registered-job){target="_blank" rel="noopener noreferrer"}.

## Backing Up Before an Update

Watchtower updates a container by stopping it and starting a replacement.
If you want a backup first, the backup has to finish before the update begins.

### Run Watchtower After a Backup Job

A job can depend on another job.
When you start a job with `docker compose run`, Compose first runs the jobs it depends on and checks their conditions.
With `condition: service_completed_successfully`, a Watchtower job starts only if the backup job exited without an error.

This example backs up the `app` service's volume with [offen/docker-volume-backup](https://offen.github.io/docker-volume-backup/){target="_blank" rel="noopener noreferrer"} and then runs Watchtower once:

```yaml
services:
  app:
    image: IMAGE
    volumes:
      - appdata:/data

jobs:
  backup:
    image: offen/docker-volume-backup:v2
    entrypoint: ["backup"]
    labels:
      com.centurylinklabs.watchtower.enable: "false"
    volumes:
      - appdata:/backup/appdata:ro
      - ./backups:/archive
    triggers:
      manual: true

  watchtower:
    image: nickfedor/watchtower:latest
    command: ["--run-once"]
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    depends_on:
      backup:
        condition: service_completed_successfully
    triggers:
      manual: true

volumes:
  appdata:
```

Replace *`IMAGE`* with the image of the service you want to back up.

Start the chain from the project directory:

```bash
docker compose run watchtower
```

If the backup fails, Compose reports that `backup` didn't complete successfully and doesn't start Watchtower.

To run the chain on a schedule, start it from the host, for example with a crontab entry that runs it every night at 03:00:

```text
0 3 * * * docker compose --project-directory PROJECT_DIR run watchtower
```

Replace *`PROJECT_DIR`* with the absolute path of the Compose project.

A few details of the example:

- Give the `watchtower` job no `schedule` of its own. Docker Engine ignores `depends_on` when it fires a scheduled job, so a scheduled Watchtower job wouldn't wait for the backup. A scheduled job whose `depends_on` names another job also makes `docker compose up` fail with a `no such service` error.
- The offen image normally runs its own scheduler. `entrypoint: ["backup"]` runs a single backup and exits instead.
- A job that runs as another job's dependency is an ordinary container without the `com.docker.job.id` label, named like a service container. Here it's `PROJECT-backup-1`, where *`PROJECT`* is the Compose project name. It's left stopped after each run. The `com.centurylinklabs.watchtower.enable: "false"` label keeps any Watchtower that includes stopped containers from recreating or starting it.
- Watchtower runs with the [Run Once](../../configuration/scheduling/index.md#run_once) configuration option, so it checks and updates the containers it selects and then exits. Narrow its selection with the usual [container selection](../../configuration/container-selection/index.md) options.

!!! Note
    offen archives the volume while `app` is running. For a database, follow offen's documentation, for example to dump the database or stop its container during the backup.

### Back Up a Single Container With a Pre-Update Hook

A [pre-update lifecycle hook](../lifecycle-hooks/index.md) runs a command inside a container just before Watchtower stops it.
Watchtower goes ahead with the update only if the command succeeds.
Exit code 75 skips the update.
Any other failure marks the update as failed, as [Exit Code Handling](../lifecycle-hooks/index.md#exit_code_handling) describes.

```yaml
services:
  app:
    image: IMAGE
    labels:
      com.centurylinklabs.watchtower.lifecycle.pre-update: "/usr/local/bin/backup.sh"
```

Replace *`IMAGE`* with the service's image, which must include the backup script.
[Lifecycle hooks](../lifecycle-hooks/index.md#enabling_lifecycle_hooks) must also be enabled in Watchtower.
