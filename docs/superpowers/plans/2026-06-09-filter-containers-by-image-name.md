# Filtrage des containers par nom d'image — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Permettre d'inclure et d'exclure des containers de la surveillance Watchtower en fonction du **nom de leur image** (avec support regex), en miroir du filtrage existant par nom de container.

**Architecture:** Deux nouveaux filtres composables (`FilterByImageNames`, `FilterByDisableImageNames`) dans `pkg/filters`, opérant sur `c.ImageName()` via le matcher générique existant `matchesName`. Ils sont chaînés dans `BuildFilter` juste après les filtres de nom. Deux nouveaux flags CLI/env (`--image-names`, `--disable-image-names`) sont lus dans `cmd/root.go` et passés à `BuildFilter`.

**Tech Stack:** Go, Cobra/pflag (flags), Mockery (mocks de test), Testify (assertions). Conventions `CONTRIBUTING.md` : `make fmt`, `make lint`, `make test`, Conventional Commits, commits signés GPG.

---

## File Structure

- `pkg/filters/filters.go` — **Modifié** : ajout de `FilterByImageNames` et `FilterByDisableImageNames` ; extension de `BuildFilter` (signature + chaînage + description).
- `pkg/filters/filters_test.go` — **Modifié** : nouveaux tests des filtres image + mise à jour des appels `BuildFilter` existants + nouveau test d'intégration.
- `pkg/filters/doc.go` — **Modifié** : exemple d'appel `BuildFilter` mis à jour.
- `internal/flags/flags.go` — **Modifié** : déclaration des flags `--image-names` et `--disable-image-names`.
- `cmd/root.go` — **Modifié** : vars package-level + lecture des flags + passage à `BuildFilter`.
- `docs/configuration/arguments/index.md` — **Modifié** : documentation des deux nouveaux arguments.
- `docs/configuration/container-selection/index.md` — **Modifié** : mention du support regex pour le filtrage par image.

Aucune modification de l'interface `types.FilterableContainer` (`ImageName()` existe déjà) → **pas de `make mocks`**.

---

## Task 1: Nouveaux filtres `FilterByImageNames` et `FilterByDisableImageNames`

**Files:**
- Modify: `pkg/filters/filters.go` (ajout après `FilterByDisableNames`, ~ligne 191)
- Test: `pkg/filters/filters_test.go`

- [ ] **Step 1: Écrire les tests qui échouent**

Ajouter à la fin de `pkg/filters/filters_test.go` :

```go
func TestFilterByImageNames(t *testing.T) {
	t.Parallel()

	imageNames := make([]string, 0, 1)

	filter := FilterByImageNames(imageNames, nil)
	assert.Nil(t, filter)

	imageNames = append(imageNames, "nginx:latest")
	filter = FilterByImageNames(imageNames, NoFilter)
	assert.NotNil(t, filter)

	// Image matches -> kept.
	container := new(mockContainer.FilterableContainer)
	container.On("Name").Return("web")
	container.On("ImageName").Return("nginx:latest")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	// Image does not match -> excluded.
	container = new(mockContainer.FilterableContainer)
	container.On("Name").Return("cache")
	container.On("ImageName").Return("redis:latest")
	assert.False(t, filter(container))
	container.AssertExpectations(t)
}

func TestFilterByImageNamesRegex(t *testing.T) {
	t.Parallel()

	filter := FilterByImageNames([]string{"nginx:.*"}, NoFilter)
	assert.NotNil(t, filter)

	// Anchored regex matches any nginx tag.
	container := new(mockContainer.FilterableContainer)
	container.On("Name").Return("web")
	container.On("ImageName").Return("nginx:1.25")
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	// Anchored regex does not match a different image name.
	container = new(mockContainer.FilterableContainer)
	container.On("Name").Return("web")
	container.On("ImageName").Return("nginxx:1.25")
	assert.False(t, filter(container))
	container.AssertExpectations(t)
}

func TestFilterByDisableImageNames(t *testing.T) {
	t.Parallel()

	disableImageNames := make([]string, 0, 1)

	filter := FilterByDisableImageNames(disableImageNames, nil)
	assert.Nil(t, filter)

	disableImageNames = append(disableImageNames, "nginx:latest")
	filter = FilterByDisableImageNames(disableImageNames, NoFilter)
	assert.NotNil(t, filter)

	// Excluded image.
	container := new(mockContainer.FilterableContainer)
	container.On("Name").Return("web")
	container.On("ImageName").Return("nginx:latest")
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	// Non-excluded image passes through baseFilter.
	container = new(mockContainer.FilterableContainer)
	container.On("Name").Return("cache")
	container.On("ImageName").Return("redis:latest")
	assert.True(t, filter(container))
	container.AssertExpectations(t)
}
```

- [ ] **Step 2: Lancer les tests pour vérifier qu'ils échouent**

Run: `go test ./pkg/filters/ -run 'TestFilterByImageNames|TestFilterByImageNamesRegex|TestFilterByDisableImageNames' -v`
Expected: FAIL à la compilation (`undefined: FilterByImageNames`, `undefined: FilterByDisableImageNames`).

- [ ] **Step 3: Implémenter les deux filtres**

Dans `pkg/filters/filters.go`, juste après la fonction `FilterByDisableNames` (qui se termine ~ligne 191, avant `FilterByEnableLabel`), insérer :

```go
// FilterByImageNames selects containers whose image matches specified names.
//
// Parameters:
//   - imageNames: List of image names or regex patterns to match.
//   - baseFilter: Base filter to chain.
//
// Returns:
//   - types.Filter: Filter function combining image name check with base filter.
func FilterByImageNames(imageNames []string, baseFilter types.Filter) types.Filter {
	if len(imageNames) == 0 {
		return baseFilter
	}

	return func(c types.FilterableContainer) bool {
		imageName := c.ImageName()
		clog := logrus.WithFields(logrus.Fields{
			"container":  c.Name(),
			"image":      imageName,
			"imageNames": imageNames,
		})

		for _, pattern := range imageNames {
			if matchesName(imageName, pattern) {
				clog.Debug("Matched container by image name/pattern")

				return baseFilter(c)
			}
		}

		clog.Debug("Container image did not match any filter")

		return false
	}
}

// FilterByDisableImageNames excludes containers whose image matches specified names.
//
// Parameters:
//   - disableImageNames: Image names or regex patterns to exclude.
//   - baseFilter: Base filter to chain.
//
// Returns:
//   - types.Filter: Filter function excluding image names and applying base filter.
func FilterByDisableImageNames(disableImageNames []string, baseFilter types.Filter) types.Filter {
	if len(disableImageNames) == 0 {
		return baseFilter
	}

	return func(c types.FilterableContainer) bool {
		imageName := c.ImageName()
		clog := logrus.WithFields(logrus.Fields{
			"container":         c.Name(),
			"image":             imageName,
			"disableImageNames": disableImageNames,
		})

		for _, pattern := range disableImageNames {
			if matchesName(imageName, pattern) {
				clog.Debug("Container excluded by disable image name/pattern")

				return false
			}
		}

		clog.Debug("Container not excluded by disable image names")

		return baseFilter(c)
	}
}
```

- [ ] **Step 4: Lancer les tests pour vérifier qu'ils passent**

Run: `go test ./pkg/filters/ -run 'TestFilterByImageNames|TestFilterByImageNamesRegex|TestFilterByDisableImageNames' -v`
Expected: PASS (les 3 tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/filters/filters.go pkg/filters/filters_test.go
git commit -S -m "feat(filters): add image-name include/exclude filters"
```

---

## Task 2: Flags CLI `--image-names` et `--disable-image-names`

**Files:**
- Modify: `internal/flags/flags.go` (après le bloc `disable-containers`, ~ligne 147)

- [ ] **Step 1: Déclarer les deux flags**

Dans `internal/flags/flags.go`, immédiatement après le bloc `flags.StringSliceP("disable-containers", ...)` (se terminant ~ligne 147), insérer :

```go
	flags.StringSlice(
		"image-names",
		filterEmptyStrings(
			regexp.MustCompile("[, ]+").Split(envString("WATCHTOWER_IMAGE_NAMES"), -1),
		),
		"Comma-separated list of image names (regex supported) to include in watching.")

	flags.StringSlice(
		"disable-image-names",
		filterEmptyStrings(
			regexp.MustCompile("[, ]+").Split(envString("WATCHTOWER_DISABLE_IMAGE_NAMES"), -1),
		),
		"Comma-separated list of image names (regex supported) to explicitly exclude from watching.")
```

(Pas de short flag : `-x` et les lettres pertinentes sont déjà prises. `regexp` et les helpers `filterEmptyStrings`/`envString` sont déjà importés/définis dans ce fichier.)

- [ ] **Step 2: Vérifier la compilation**

Run: `go build ./...`
Expected: succès, aucune erreur.

- [ ] **Step 3: Vérifier que les flags sont enregistrés**

Run: `go run . --help 2>&1 | grep -E 'image-names|disable-image-names'`
Expected: deux lignes affichant `--image-names` et `--disable-image-names` avec leurs descriptions.

- [ ] **Step 4: Commit**

```bash
git add internal/flags/flags.go
git commit -S -m "feat(flags): add image-names and disable-image-names flags"
```

---

## Task 3: Brancher les filtres image dans `BuildFilter` et `cmd/root.go`

**Files:**
- Modify: `pkg/filters/filters.go` (`BuildFilter`, ~lignes 363-450)
- Modify: `pkg/filters/doc.go` (exemple, ~ligne 10)
- Modify: `pkg/filters/filters_test.go` (4 appels existants + 1 nouveau test)
- Modify: `cmd/root.go` (vars ~ligne 92, lecture ~ligne 303, appel ~ligne 515)

> Note : ce changement de signature de `BuildFilter` casse la compilation de tous ses appelants tant qu'ils ne sont pas tous mis à jour. On modifie donc `BuildFilter`, `doc.go`, `root.go` et tous les tests `BuildFilter` dans le **même commit** pour garder un état compilable.

- [ ] **Step 1: Écrire le test d'intégration qui échouera**

Ajouter à la fin de `pkg/filters/filters_test.go` :

```go
func TestBuildFilterImageNames(t *testing.T) {
	t.Parallel()

	filter, desc := BuildFilter(
		nil, nil,
		[]string{"nginx:.*", "redis:.*"},
		[]string{"redis:latest"},
		false, "",
	)
	assert.Contains(t, desc, "which image matches")
	assert.Contains(t, desc, "nginx:.*")
	assert.Contains(t, desc, "whose image is not one of")
	assert.Contains(t, desc, "redis:latest")

	// Image matches an include pattern and is not disabled -> kept.
	container := new(mockContainer.FilterableContainer)
	container.On("IsWatchtower").Return(false).Maybe()
	container.On("Name").Return("/web").Maybe()
	container.On("ImageName").Return("nginx:1.25").Maybe()
	container.On("Enabled").Return(false, false).Maybe()
	container.On("Scope").Return("", false).Maybe()
	assert.True(t, filter(container))
	container.AssertExpectations(t)

	// Image matches no include pattern -> excluded.
	container = new(mockContainer.FilterableContainer)
	container.On("IsWatchtower").Return(false).Maybe()
	container.On("Name").Return("/api").Maybe()
	container.On("ImageName").Return("api:latest").Maybe()
	container.On("Enabled").Return(false, false).Maybe()
	container.On("Scope").Return("", false).Maybe()
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	// Image matches an include pattern but is explicitly disabled -> excluded.
	container = new(mockContainer.FilterableContainer)
	container.On("IsWatchtower").Return(false).Maybe()
	container.On("Name").Return("/cache").Maybe()
	container.On("ImageName").Return("redis:latest").Maybe()
	container.On("Enabled").Return(false, false).Maybe()
	container.On("Scope").Return("", false).Maybe()
	assert.False(t, filter(container))
	container.AssertExpectations(t)

	// Image matches include pattern and disable pattern does not match -> kept.
	container = new(mockContainer.FilterableContainer)
	container.On("IsWatchtower").Return(false).Maybe()
	container.On("Name").Return("/cache2").Maybe()
	container.On("ImageName").Return("redis:7").Maybe()
	container.On("Enabled").Return(false, false).Maybe()
	container.On("Scope").Return("", false).Maybe()
	assert.True(t, filter(container))
	container.AssertExpectations(t)
}
```

- [ ] **Step 2: Lancer le test pour vérifier qu'il échoue**

Run: `go test ./pkg/filters/ -run TestBuildFilterImageNames -v`
Expected: FAIL à la compilation (trop d'arguments pour `BuildFilter` — la signature n'a pas encore les params image).

- [ ] **Step 3: Étendre la signature et le chaînage de `BuildFilter`**

Dans `pkg/filters/filters.go`, remplacer la signature et le bloc de doc de `BuildFilter` :

```go
// BuildFilter constructs a composite filter for containers.
//
// Parameters:
//   - normalizedNames: Normalized names or regex patterns to include.
//   - normalizedDisableNames: Normalized names or regex patterns to exclude.
//   - imageNames: Image names or regex patterns to include.
//   - disableImageNames: Image names or regex patterns to exclude.
//   - enableLabel: Require enable label if true.
//   - scope: Scope to match.
//
// Returns:
//   - types.Filter: Combined filter function.
//   - string: Description of the filter.
func BuildFilter(
	normalizedNames []string,
	normalizedDisableNames []string,
	imageNames []string,
	disableImageNames []string,
	enableLabel bool,
	scope string,
) (types.Filter, string) {
```

Dans le corps, étendre le champ de log `clog` initial pour inclure les nouvelles entrées :

```go
	clog := logrus.WithFields(logrus.Fields{
		"names":             normalizedNames,
		"disableNames":      normalizedDisableNames,
		"imageNames":        imageNames,
		"disableImageNames": disableImageNames,
		"enableLabel":       enableLabel,
		"scope":             scope,
	})
```

Ajouter le chaînage des deux filtres image juste après les deux filtres de nom :

```go
	filter := NoFilter
	filter = FilterByNames(normalizedNames, filter)
	filter = FilterByDisableNames(normalizedDisableNames, filter)
	filter = FilterByImageNames(imageNames, filter)
	filter = FilterByDisableImageNames(disableImageNames, filter)
```

- [ ] **Step 4: Ajouter les fragments de description**

Dans `BuildFilter`, juste après le bloc « Add disable-name-based filter description. » (le `if len(normalizedDisableNames) > 0 { ... }`) et avant le `if enableLabel {`, insérer :

```go
	// Add image-name-based filter description.
	if len(imageNames) > 0 {
		stringBuilder.WriteString("which image matches \"")

		for i, n := range imageNames {
			stringBuilder.WriteString(n)

			if i < len(imageNames)-1 {
				stringBuilder.WriteString(`" or "`)
			}
		}

		stringBuilder.WriteString(`", `)
	}

	// Add disable-image-name-based filter description.
	if len(disableImageNames) > 0 {
		stringBuilder.WriteString("whose image is not one of \"")

		for i, n := range disableImageNames {
			stringBuilder.WriteString(n)

			if i < len(disableImageNames)-1 {
				stringBuilder.WriteString(`" or "`)
			}
		}

		stringBuilder.WriteString(`", `)
	}
```

- [ ] **Step 5: Mettre à jour l'exemple dans `doc.go`**

Dans `pkg/filters/doc.go`, remplacer la ligne d'exemple :

```go
//	filter, desc := filters.BuildFilter(names, disableNames, true, "scope")
```

par :

```go
//	filter, desc := filters.BuildFilter(names, disableNames, imageNames, disableImageNames, true, "scope")
```

- [ ] **Step 6: Mettre à jour les appels `BuildFilter` existants dans les tests**

Dans `pkg/filters/filters_test.go`, appliquer ces 4 remplacements (ajout de `nil, nil` pour les params image, entre les disable-names et `enableLabel`) :

- `BuildFilter(nil, nil, false, "none")` → `BuildFilter(nil, nil, nil, nil, false, "none")`
- `BuildFilter(names, []string{}, false, "")` → `BuildFilter(names, []string{}, nil, nil, false, "")`
- `BuildFilter(names, []string{}, true, "")` → `BuildFilter(names, []string{}, nil, nil, true, "")`
- `BuildFilter([]string{}, []string{"excluded", "notfound"}, false, "")` → `BuildFilter([]string{}, []string{"excluded", "notfound"}, nil, nil, false, "")`

- [ ] **Step 7: Déclarer les vars package-level dans `cmd/root.go`**

Dans `cmd/root.go`, juste après le bloc de déclaration `disableContainers []string` (~ligne 92), insérer :

```go
	// imageNames is a slice of image names (regex supported) to include in watching.
	//
	// It is populated in preRun from the --image-names flag or the WATCHTOWER_IMAGE_NAMES environment variable,
	// restricting updates to containers whose image matches one of the patterns.
	imageNames []string

	// disableImageNames is a slice of image names (regex supported) explicitly excluded from watching.
	//
	// It is populated in preRun from the --disable-image-names flag or the WATCHTOWER_DISABLE_IMAGE_NAMES environment variable,
	// allowing users to blacklist containers by image name from Watchtower's operations.
	disableImageNames []string
```

- [ ] **Step 8: Lire les flags dans preRun de `cmd/root.go`**

Dans `cmd/root.go`, juste après le bloc qui normalise `disableContainers` (la boucle se terminant ~ligne 303), insérer :

```go
	// Set image names included in or excluded from Watchtower's handling.
	imageNames, _ = flagsSet.GetStringSlice("image-names")
	for i := range imageNames {
		imageNames[i] = strings.TrimSpace(imageNames[i])
	}

	disableImageNames, _ = flagsSet.GetStringSlice("disable-image-names")
	for i := range disableImageNames {
		disableImageNames[i] = strings.TrimSpace(disableImageNames[i])
	}
```

(Vérifier que `"strings"` est déjà importé dans `cmd/root.go` ; l'ajouter au bloc d'imports si absent.)

- [ ] **Step 9: Passer les nouveaux arguments à `BuildFilter` dans `run`**

Dans `cmd/root.go`, remplacer l'appel (~ligne 515) :

```go
	filter, filterDesc := filters.BuildFilter(
		normalizedContainerNames,
		disableContainers, // Normalized container names
		enableLabel,
		scope,
	)
```

par :

```go
	filter, filterDesc := filters.BuildFilter(
		normalizedContainerNames,
		disableContainers, // Normalized container names
		imageNames,
		disableImageNames,
		enableLabel,
		scope,
	)
```

- [ ] **Step 10: Lancer le test d'intégration et la suite du package**

Run: `go test ./pkg/filters/ -v`
Expected: PASS, y compris `TestBuildFilterImageNames` et tous les tests `BuildFilter` mis à jour.

- [ ] **Step 11: Vérifier la compilation globale**

Run: `go build ./...`
Expected: succès.

- [ ] **Step 12: Commit**

```bash
git add pkg/filters/filters.go pkg/filters/doc.go pkg/filters/filters_test.go cmd/root.go
git commit -S -m "feat(filters): wire image-name filters into BuildFilter and CLI"
```

---

## Task 4: Documentation

**Files:**
- Modify: `docs/configuration/arguments/index.md` (après la section « Disable Specific Containers », ~ligne 433)
- Modify: `docs/configuration/container-selection/index.md` (section « Regex Pattern Matching », ~ligne 91)

- [ ] **Step 1: Documenter les deux nouveaux arguments**

Dans `docs/configuration/arguments/index.md`, juste après la section « Disable Specific Containers » (le bloc Note se terminant ~ligne 433, avant « ### Scope Filter »), insérer :

```markdown
### Include Specific Images

Restricts monitoring to containers whose image name matches one of the supplied
values, even if other selection criteria would include them.

```text
            Argument: --image-names
Environment Variable: WATCHTOWER_IMAGE_NAMES
                Type: Comma- or space-separated string list
             Default: None
```

!!! Note
    Image names include the tag (for example `nginx:latest`). Regex patterns are
    supported and anchored to the **full** image name. See
    [Regex Pattern Matching](../container-selection/index.md#regex-pattern-matching)
    for details.

### Disable Specific Images

Excludes containers by image name from monitoring, even if they have the enable
label set to `true`.

```text
            Argument: --disable-image-names
Environment Variable: WATCHTOWER_DISABLE_IMAGE_NAMES
                Type: Comma- or space-separated string list
             Default: None
```

!!! Note
    Image names include the tag (for example `nginx:latest`). Regex patterns are
    supported and anchored to the **full** image name. See
    [Regex Pattern Matching](../container-selection/index.md#regex-pattern-matching)
    for details.
```

- [ ] **Step 2: Mentionner le filtrage par image dans la section regex**

Dans `docs/configuration/container-selection/index.md`, remplacer le paragraphe d'introduction de « ## Regex Pattern Matching » (~ligne 91) :

```markdown
Both container inclusion (positional arguments) and exclusion  ([`--disable-containers`/`WATCHTOWER_DISABLE_CONTAINERS`](../arguments/index.md#disable_specific_containers)) support regular expression patterns for matching container names.
```

par :

```markdown
Both container inclusion (positional arguments) and exclusion  ([`--disable-containers`/`WATCHTOWER_DISABLE_CONTAINERS`](../arguments/index.md#disable_specific_containers)) support regular expression patterns for matching container names.

Image-name selection ([`--image-names`/`WATCHTOWER_IMAGE_NAMES`](../arguments/index.md#include_specific_images) and [`--disable-image-names`/`WATCHTOWER_DISABLE_IMAGE_NAMES`](../arguments/index.md#disable_specific_images)) supports the same regex syntax, matching against the **full image name including its tag** (for example `nginx:latest`). To match every tag of an image, use a pattern such as `nginx:.*`.
```

- [ ] **Step 3: Vérifier le rendu Markdown**

Run: `git diff --stat docs/`
Expected: les deux fichiers docs apparaissent comme modifiés ; relire visuellement les deux diffs.

> Rappel ancres : MkDocs est configuré avec `toc.separator = "_"` (`build/mkdocs/mkdocs.yaml`). Les ancres sont donc en minuscules avec **underscores** : le titre « Include Specific Images » → `#include_specific_images`, « Disable Specific Images » → `#disable_specific_images`. Les liens du Step 2 utilisent déjà cette convention.

- [ ] **Step 4: Commit**

```bash
git add docs/configuration/arguments/index.md docs/configuration/container-selection/index.md
git commit -S -m "docs(config): document image-name include/exclude filters"
```

---

## Task 5: Vérification finale (conventions CONTRIBUTING.md)

**Files:** aucun changement de code attendu (sauf corrections de lint/format).

- [ ] **Step 1: Formater**

Run: `make fmt`
Expected: aucune erreur ; si des fichiers sont reformatés, les inspecter.

- [ ] **Step 2: Linter**

Run: `make lint`
Expected: aucun problème signalé (le lint lance `--fix` ; vérifier qu'aucune correction non triviale n'est introduite).

- [ ] **Step 3: Suite de tests complète**

Run: `make test`
Expected: PASS pour l'ensemble des packages dans la limite de 30 s.

- [ ] **Step 4: Commit éventuel des corrections de format/lint**

```bash
git add -A
git commit -S -m "style: apply formatting and lint fixes for image-name filters"
```

(À n'exécuter que si `make fmt`/`make lint` ont modifié des fichiers ; sinon ignorer.)
