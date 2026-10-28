package rebuild

import (
	"bytes"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"

	composeProject "github.com/nicholas-fedor/watchtower/internal/compose/project"
	mockUpdate "github.com/nicholas-fedor/watchtower/internal/update/mocks"
	"github.com/nicholas-fedor/watchtower/pkg/types"
)

// batchTestContainer returns a container whose name and ID are the given name.
// It links to the named containers.
func batchTestContainer(name string, links ...string) types.Container {
	containerLinks := make([]string, 0, len(links))
	for _, link := range links {
		containerLinks = append(containerLinks, "/"+link+":"+link)
	}

	return mockUpdate.CreateMockContainerWithLinks(name, "/"+name, name+":latest", time.Now(), containerLinks, nil)
}

// testBatch returns a batch for the Compose project in the given directory
// with the given members.
func testBatch(dir string, members ...types.Container) *composeBatch {
	batch := &composeBatch{ref: composeProject.ProjectRef{Dir: dir, Name: dir}}
	for _, c := range members {
		batch.members = append(batch.members, composeMember{container: c})
	}

	return batch
}

// batchDirs returns the project directory of each batch, in order.
func batchDirs(batches []*composeBatch) []string {
	dirs := make([]string, 0, len(batches))
	for _, batch := range batches {
		dirs = append(dirs, batch.ref.Dir)
	}

	return dirs
}

// orderBatches returns the batches in the dependency order of the containers.
func orderBatches(log *zerolog.Logger, batches []*composeBatch, containers []types.Container) []*composeBatch {
	return (&composeBatches{firstSeen: batches}).inDependencyOrder(log, containers, false)
}

// TestComposeBatches_add verifies that containers are grouped into one batch
// per Compose project in the order each project is first seen. Projects that
// share a directory but differ in name or config files stay separate.
func TestComposeBatches_add(t *testing.T) {
	t.Parallel()

	app, db, web := batchTestContainer("app"), batchTestContainer("db"), batchTestContainer("web")
	jobs, cron := batchTestContainer("jobs"), batchTestContainer("cron")

	project := composeProject.ProjectRef{Dir: "/stacks/one", Name: "one"}
	other := composeProject.ProjectRef{Dir: "/stacks/two", Name: "two"}
	renamed := composeProject.ProjectRef{Dir: "/stacks/one", Name: "renamed"}
	override := composeProject.ProjectRef{Dir: "/stacks/one", Name: "one", ConfigFiles: []string{"compose.prod.yaml"}}

	var batches composeBatches

	batches.add(project, composeMember{container: app})
	batches.add(other, composeMember{container: web})
	batches.add(project, composeMember{container: db})
	batches.add(renamed, composeMember{container: jobs})
	batches.add(override, composeMember{container: cron})

	got := make([][]types.ContainerID, 0, len(batches.firstSeen))
	for _, batch := range batches.firstSeen {
		ids := make([]types.ContainerID, 0, len(batch.members))
		for _, member := range batch.members {
			ids = append(ids, member.container.ID())
		}

		got = append(got, ids)
	}

	assert.Equal(t, [][]types.ContainerID{{"app", "db"}, {"web"}, {"jobs"}, {"cron"}}, got)

	refs := make([]composeProject.ProjectRef, 0, len(batches.firstSeen))
	for _, batch := range batches.firstSeen {
		refs = append(refs, batch.ref)
	}

	assert.Equal(t, []composeProject.ProjectRef{project, other, renamed, override}, refs)
}

// TestComposeBatches_inDependencyOrder verifies that a batch runs after every
// batch that holds a dependency of one of its members. Batches without a
// dependency between them keep their first-seen order, and a lower first-seen
// position wins among batches that are ready together. Batches in a dependency
// cycle follow the others in first-seen order.
func TestComposeBatches_inDependencyOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// build returns the batches in first-seen order and every container.
		build       func() ([]*composeBatch, []types.Container)
		want        []string
		wantWarning string
	}{
		{
			name: "a single batch",
			build: func() ([]*composeBatch, []types.Container) {
				app := batchTestContainer("app")

				return []*composeBatch{testBatch("a", app)}, []types.Container{app}
			},
			want: []string{"a"},
		},
		{
			name: "independent batches keep first-seen order",
			build: func() ([]*composeBatch, []types.Container) {
				app, web := batchTestContainer("app"), batchTestContainer("web")

				return []*composeBatch{testBatch("a", app), testBatch("b", web)}, []types.Container{app, web}
			},
			want: []string{"a", "b"},
		},
		{
			name: "a dependency seen later runs first",
			build: func() ([]*composeBatch, []types.Container) {
				app, db := batchTestContainer("app", "db"), batchTestContainer("db")

				return []*composeBatch{testBatch("a", app), testBatch("b", db)}, []types.Container{app, db}
			},
			want: []string{"b", "a"},
		},
		{
			name: "a project with an independent service still waits for its dependency",
			build: func() ([]*composeBatch, []types.Container) {
				solo, app, db := batchTestContainer("solo"), batchTestContainer("app", "db"), batchTestContainer("db")

				return []*composeBatch{testBatch("a", solo, app), testBatch("b", db)}, []types.Container{solo, app, db}
			},
			want: []string{"b", "a"},
		},
		{
			name: "a dependency inside one batch adds no order",
			build: func() ([]*composeBatch, []types.Container) {
				app, db, web := batchTestContainer("app", "db"), batchTestContainer("db"), batchTestContainer("web")

				return []*composeBatch{testBatch("a", app, db), testBatch("b", web)}, []types.Container{app, db, web}
			},
			want: []string{"a", "b"},
		},
		{
			name: "ready batches run in first-seen order",
			build: func() ([]*composeBatch, []types.Container) {
				front := batchTestContainer("front", "api", "worker")
				api, worker, db := batchTestContainer("api", "db"), batchTestContainer("worker", "db"), batchTestContainer("db")

				return []*composeBatch{
					testBatch("a", front), testBatch("b", worker), testBatch("c", api), testBatch("d", db),
				}, []types.Container{front, api, worker, db}
			},
			want: []string{"d", "b", "c", "a"},
		},
		{
			name: "a cycle follows the resolvable batches",
			build: func() ([]*composeBatch, []types.Container) {
				x, y, solo := batchTestContainer("x", "y"), batchTestContainer("y", "x"), batchTestContainer("solo")

				return []*composeBatch{testBatch("a", x), testBatch("b", y), testBatch("c", solo)},
					[]types.Container{x, y, solo}
			},
			want:        []string{"c", "a", "b"},
			wantWarning: "Compose batch dependencies are cyclic",
		},
		{
			name: "a dependency lookup failure keeps first-seen order",
			build: func() ([]*composeBatch, []types.Container) {
				first, second := batchTestContainer("app"), batchTestContainer("app")
				db := batchTestContainer("db")

				return []*composeBatch{testBatch("a", first), testBatch("b", db)}, []types.Container{first, second, db}
			},
			want:        []string{"a", "b"},
			wantWarning: "Could not order Compose batches by dependencies",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer

			log := zerolog.New(&buf).Level(zerolog.WarnLevel)
			batches, containers := tt.build()

			assert.Equal(t, tt.want, batchDirs(orderBatches(&log, batches, containers)))

			if tt.wantWarning == "" {
				assert.Empty(t, buf.String())
			} else {
				assert.Contains(t, buf.String(), tt.wantWarning)
			}
		})
	}
}
