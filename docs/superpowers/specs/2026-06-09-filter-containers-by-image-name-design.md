# Filtrage des containers par nom d'image — Design

## Contexte

Watchtower sélectionne les containers à surveiller via un système de filtres composables
(`pkg/filters/filters.go`). Aujourd'hui, on peut :

- **Inclure** des containers par leur **nom** (arguments positionnels → `FilterByNames`)
- **Exclure** des containers par leur **nom** (`--disable-containers` / `-x` /
  `WATCHTOWER_DISABLE_CONTAINERS` → `FilterByDisableNames`)

Les deux supportent les regex ancrées (`^pattern$`) via le matcher `matchesName`.

**Objectif** : offrir la même capacité de filtrage, mais sur le **nom de l'image** du
container plutôt que sur le nom du container.

## Décisions

- **Portée** : inclusion **et** exclusion par nom d'image (symétrie complète).
- **Matching** : strictement identique au filtre par nom — regex ancrée `^pattern$` sur
  la chaîne complète retournée par `ImageName()`, qui **inclut le tag**
  (ex. `nginx:latest`, `ghcr.io/foo/bar:1.25`). Pour matcher toutes les versions d'une
  image, l'utilisateur écrit `nginx:.*` ou `nginx.*`. Aucun « smart matching » avec/sans
  tag — comportement cohérent et prévisible avec le filtre par nom.
- **Flags** :
  - Inclusion : `--image-names` (env `WATCHTOWER_IMAGE_NAMES`)
  - Exclusion : `--disable-image-names` (env `WATCHTOWER_DISABLE_IMAGE_NAMES`)
  - Pas de short flag (les lettres pertinentes sont déjà prises, ex. `-x`).
- **Signature `BuildFilter`** : extension positionnelle (cohérente avec le style existant),
  plutôt qu'un struct `FilterOptions`. Alternative écartée car elle imposerait un refactor
  de tous les call sites/tests sans bénéfice demandé.

## Composants

### 1. Nouveaux filtres — `pkg/filters/filters.go`

Deux fonctions calquées sur `FilterByNames` / `FilterByDisableNames`, mais opérant sur
`c.ImageName()` :

```go
// FilterByImageNames selects containers whose image matches one of the patterns.
func FilterByImageNames(imageNames []string, baseFilter types.Filter) types.Filter

// FilterByDisableImageNames excludes containers whose image matches one of the patterns.
func FilterByDisableImageNames(disableImageNames []string, baseFilter types.Filter) types.Filter
```

Comportement :

- Liste vide → retourne `baseFilter` inchangé (pass-through).
- `FilterByImageNames` : si une image match un pattern → `baseFilter(c)`, sinon `false`.
- `FilterByDisableImageNames` : si une image match un pattern → `false` (exclu), sinon
  `baseFilter(c)`.
- Réutilisation du matcher générique existant `matchesName(value, pattern)` : il fait déjà
  « exact d'abord, puis regex ancrée », et opère sur n'importe quelle chaîne. Le
  `strings.TrimPrefix(pattern, "/")` qu'il applique est inoffensif pour un nom d'image
  (les images ne commencent pas par `/`).
- Logs `Debug` cohérents avec les filtres existants (champs `container`, `image`,
  `imageNames` / `disableImageNames`).

Aucune modification de l'interface `types.FilterableContainer` : `ImageName()` y existe déjà.

### 2. Composition — `BuildFilter`

Nouvelle signature :

```go
func BuildFilter(
    normalizedNames []string,
    normalizedDisableNames []string,
    imageNames []string,
    disableImageNames []string,
    enableLabel bool,
    scope string,
) (types.Filter, string)
```

Les deux nouveaux filtres sont chaînés **juste après** les filtres de nom :

```go
filter = FilterByNames(normalizedNames, filter)
filter = FilterByDisableNames(normalizedDisableNames, filter)
filter = FilterByImageNames(imageNames, filter)
filter = FilterByDisableImageNames(disableImageNames, filter)
// ... (enableLabel, scope, disabled label, old watchtower) inchangés
```

Description (`filterDesc`) : ajout de fragments sur le modèle existant, p. ex.
`which image matches "<...>"` et `whose image is not one of "<...>"`.

### 3. Flags — `internal/flags/flags.go`

Deux `StringSliceP` calqués sur `--disable-containers`, défaut lu depuis l'env via le même
pattern (`regexp.MustCompile("[, ]+").Split(...)` + `filterEmptyStrings`) :

```go
flags.StringSlice(
    "image-names",
    filterEmptyStrings(regexp.MustCompile("[, ]+").Split(envString("WATCHTOWER_IMAGE_NAMES"), -1)),
    "Comma-separated list of image names (regex supported) to include in watching.",
)
flags.StringSlice(
    "disable-image-names",
    filterEmptyStrings(regexp.MustCompile("[, ]+").Split(envString("WATCHTOWER_DISABLE_IMAGE_NAMES"), -1)),
    "Comma-separated list of image names (regex supported) to explicitly exclude from watching.",
)
```

### 4. Câblage — `cmd/root.go`

- Déclaration de deux vars package-level (`imageNames`, `disableImageNames`) sur le modèle
  de `disableContainers`.
- Lecture dans la phase pré-run :
  ```go
  imageNames, _ = flagsSet.GetStringSlice("image-names")
  disableImageNames, _ = flagsSet.GetStringSlice("disable-image-names")
  ```
- `strings.TrimSpace` sur chaque valeur (pas de `NormalizeContainerName` : les images
  n'ont pas de leading slash à retirer).
- Passage à `BuildFilter` avec les deux nouveaux arguments.

## Tests — `pkg/filters/filters_test.go`

- `TestFilterByImageNames` : match exact, regex (`nginx:.*`), liste de plusieurs patterns,
  trailing/leading whitespace déjà trimmé, liste vide → pass-through.
- `TestFilterByDisableImageNames` : exclusion exacte, exclusion regex, container non exclu
  passe au `baseFilter`, liste vide → pass-through.
- Mise à jour des appels `BuildFilter` existants (ajout des deux nouveaux args, en
  `nil` / `[]string{}`) :
  - `TestBuildFilterNoneScope`
  - `TestBuildFilter`
  - `TestBuildFilterEnableLabel`
  - `TestBuildFilterDisableContainer`
- `TestBuildFilterImageNames` : nouveau cas couvrant inclusion **et** exclusion par image,
  et vérification de la `filterDesc`.

Mocks : utiliser le `FilterableContainer` de test existant en renseignant `ImageName()`.

## Documentation

- `docs/configuration/arguments/index.md` : deux nouvelles sections (« Image Names » et
  « Disable Image Names ») sur le modèle de « Disable Specific Containers », avec bloc
  `Argument / Environment Variable / Type / Default` et note renvoyant vers la section
  Regex.
- `docs/configuration/container-selection/index.md` : mention que le filtrage par image
  supporte aussi les regex (renvoi vers la section « Regex Pattern Matching » existante).

## Hors périmètre (YAGNI)

- Pas de « smart matching » avec/sans tag.
- Pas de struct `FilterOptions` / refactor de la signature au-delà de l'ajout des deux
  paramètres.
- Pas de filtrage par digest, registry ou label d'image autre que le nom.
