package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"

	"github.com/musher-dev/musher-cli/internal/client"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// errNoRevision means a lineage that should hold a revision returned none.
var errNoRevision = errors.New("the platform returned no revision")

// versionRef is one revision of a component or blueprint, reduced to what the
// converge loop reasons about.
type versionRef struct {
	id         string
	revision   int
	status     string
	document   json.RawMessage
	rowVersion int
}

// versionStore adapts the component or blueprint routes to one converge loop.
//
// Components and blueprints share the same lifecycle — a lineage named by a
// slug, numbered revisions, at most one open draft, publish by revision — so
// the read-before-write rules are written once and each resource supplies its
// own transport.
type versionStore struct {
	// resource is DocumentComponent or DocumentBlueprint.
	resource string
	slug     string
	// conflictExtension is the problem extension naming a slug's holder.
	conflictExtension string
	source            provenance

	// desired renders the document this deploy wants at revision.
	desired func(revision int) any

	find          func(ctx context.Context) (string, error)
	create        func(ctx context.Context, revision int) (versionRef, error)
	latest        func(ctx context.Context, id string) (*client.VersionSummary, error)
	get           func(ctx context.Context, id string, revision int) (versionRef, error)
	createVersion func(ctx context.Context, id string, revision int) (versionRef, error)
	replace       func(ctx context.Context, id string, revision, rowVersion int) (versionRef, error)
	publish       func(ctx context.Context, id string, revision int) (versionRef, error)
}

// ensured is the outcome of converging one lineage.
type ensured struct {
	versionRef

	// changed is true when this run wrote anything.
	changed bool
}

// ensure converges one lineage onto the desired document and returns the
// published revision that holds it.
//
// The rules, applied in order, make a re-run converge rather than duplicate:
//
//   - no lineage holds the slug: create it; a 409 resource-conflict names the
//     holder, which is adopted;
//   - the newest revision is an open draft: reuse it when it already holds the
//     desired document, otherwise replace it, then publish;
//   - the newest revision is published and holds the desired document: no
//     write at all;
//   - otherwise: open revision newest+1 and publish it.
func (r *deployRun) ensure(ctx context.Context, store *versionStore) (*ensured, error) {
	id, err := store.find(ctx)
	if err != nil {
		return nil, r.storeFailure(store, "look up", err)
	}

	if id == "" {
		created, createErr := store.create(ctx, 1)
		if createErr == nil {
			return r.publishDraft(ctx, store, created)
		}

		id = adoptedID(createErr, store.conflictExtension)
		if id == "" {
			return nil, r.storeFailure(store, "create", createErr)
		}

		r.rep.Note(NoteInfo, "a %s named %q already exists; continuing with it", store.resource, store.slug)
	}

	return r.converge(ctx, store, id)
}

// converge brings an existing lineage onto the desired document.
func (r *deployRun) converge(ctx context.Context, store *versionStore, id string) (*ensured, error) {
	latest, err := store.latest(ctx, id)
	if err != nil {
		return nil, r.storeFailure(store, "read the revisions of", err)
	}

	if latest == nil {
		return r.openVersion(ctx, store, id, 1)
	}

	switch latest.Status {
	case client.VersionStatusDraft:
		return r.resumeDraft(ctx, store, id, latest.Revision)
	case client.VersionStatusPublished:
		current, getErr := store.get(ctx, id, latest.Revision)
		if getErr != nil {
			return nil, r.storeFailure(store, "read", getErr)
		}

		if sameDocument(current.document, store.desired(latest.Revision)) {
			return &ensured{versionRef: current}, nil
		}
	}

	return r.openVersion(ctx, store, id, latest.Revision+1)
}

// openVersion opens revision as a new draft and publishes it. A concurrent
// writer that opened a draft first is not a failure: the 409 names its draft,
// which this run then resumes.
func (r *deployRun) openVersion(ctx context.Context, store *versionStore, id string, revision int) (*ensured, error) {
	created, err := store.createVersion(ctx, id, revision)
	if err == nil {
		return r.publishDraft(ctx, store, created)
	}

	if problem := client.ProblemOf(err); problem != nil && problem.Slug() == client.SlugDraftAlreadyOpen {
		if open, ok := problem.ExtensionInt(client.ExtOpenDraftVersion); ok && open > 0 {
			return r.resumeDraft(ctx, store, id, open)
		}
	}

	return nil, r.storeFailure(store, "create a revision of", err)
}

// resumeDraft makes the open draft hold the desired document, then publishes.
func (r *deployRun) resumeDraft(ctx context.Context, store *versionStore, id string, revision int) (*ensured, error) {
	draft, err := store.get(ctx, id, revision)
	if err != nil {
		return nil, r.storeFailure(store, "read the draft of", err)
	}

	if !sameDocument(draft.document, store.desired(revision)) {
		draft, err = store.replace(ctx, id, revision, draft.rowVersion)
		if err != nil {
			return nil, r.storeFailure(store, "update the draft of", err)
		}
	}

	return r.publishDraft(ctx, store, draft)
}

// publishDraft releases a draft. A 409 is re-read rather than trusted: when a
// concurrent deploy already published the revision, that is the answer this
// run wanted too.
func (r *deployRun) publishDraft(ctx context.Context, store *versionStore, draft versionRef) (*ensured, error) {
	if draft.status == client.VersionStatusPublished {
		return &ensured{versionRef: draft, changed: true}, nil
	}

	published, err := store.publish(ctx, draft.id, draft.revision)
	if err == nil {
		return &ensured{versionRef: published, changed: true}, nil
	}

	if isStatus(err, http.StatusConflict) {
		reread, getErr := store.get(ctx, draft.id, draft.revision)
		if getErr == nil && reread.status == client.VersionStatusPublished {
			return &ensured{versionRef: reread, changed: true}, nil
		}
	}

	return nil, r.storeFailure(store, "publish", err)
}

// storeFailure classifies a failed library call.
func (r *deployRun) storeFailure(store *versionStore, verb string, err error) error {
	if specErr := specErrorFrom(err, store.resource, store.source); specErr != nil {
		return specErr
	}

	if isStatus(err, http.StatusConflict) {
		return &ConflictError{Resource: store.resource, Slug: store.slug, Err: err}
	}

	return repoerrors.Errorf("%s %s %q: %w", verb, store.resource, store.slug, err)
}

// adoptedID returns the id a 409 resource-conflict names in extension, or "".
func adoptedID(err error, extension string) string {
	problem := client.ProblemOf(err)
	if problem == nil || problem.Slug() != client.SlugResourceConflict {
		return ""
	}

	return problem.ExtensionString(extension)
}

// sameDocument reports whether a stored document equals the desired one,
// ignoring metadata.revision, which differs by construction.
//
// The comparison is structural on the generic JSON form, so a stored
// document that gained a field this CLI does not write — a health probe added
// in the Console, say — is not mistaken for an unchanged one.
func sameDocument(stored json.RawMessage, desired any) bool {
	if len(stored) == 0 {
		return false
	}

	encoded, err := json.Marshal(desired)
	if err != nil {
		return false
	}

	var left, right map[string]any
	if json.Unmarshal(stored, &left) != nil || json.Unmarshal(encoded, &right) != nil {
		return false
	}

	dropRevision(left)
	dropRevision(right)

	return reflect.DeepEqual(left, right)
}

func dropRevision(document map[string]any) {
	if metadata, ok := document["metadata"].(map[string]any); ok {
		delete(metadata, "revision")
	}
}

// componentStore wires the component routes into a versionStore.
func (r *deployRun) componentStore(plan *compiled) *versionStore {
	toRef := func(version *client.ComponentVersion, err error) (versionRef, error) {
		if err != nil {
			return versionRef{}, err
		}

		if version == nil {
			return versionRef{}, errNoRevision
		}

		return versionRef{
			id: version.ComponentID, revision: version.Revision, status: version.Status,
			document: version.Document, rowVersion: version.RowVersion,
		}, nil
	}

	return &versionStore{
		resource:          DocumentComponent,
		slug:              plan.slug,
		conflictExtension: client.ExtComponentID,
		source:            plan.componentSource,
		desired:           func(revision int) any { return plan.component(revision) },
		find: func(ctx context.Context) (string, error) {
			found, err := r.api.ListComponentsBySlug(ctx, r.orgID, plan.slug)
			if err != nil {
				return "", repoerrors.Errorf("list components by slug: %w", err)
			}

			if len(found) == 0 {
				return "", nil
			}

			return found[0].ID, nil
		},
		create: func(ctx context.Context, revision int) (versionRef, error) {
			return toRef(r.api.CreateComponent(ctx, r.orgID, plan.slug, plan.component(revision)))
		},
		latest: func(ctx context.Context, id string) (*client.VersionSummary, error) {
			return r.api.LatestComponentRevision(ctx, id)
		},
		get: func(ctx context.Context, id string, revision int) (versionRef, error) {
			return toRef(r.api.GetComponentVersion(ctx, id, revision))
		},
		createVersion: func(ctx context.Context, id string, revision int) (versionRef, error) {
			return toRef(r.api.CreateComponentVersion(ctx, id, plan.component(revision)))
		},
		replace: func(ctx context.Context, id string, revision, rowVersion int) (versionRef, error) {
			return toRef(r.api.ReplaceComponentVersion(ctx, id, revision, plan.component(revision), &rowVersion))
		},
		publish: func(ctx context.Context, id string, revision int) (versionRef, error) {
			return toRef(r.api.PublishComponentVersion(ctx, id, revision))
		},
	}
}

// blueprintStore wires the blueprint routes into a versionStore. The desired
// document pins the component revision ensureComponent settled on.
func (r *deployRun) blueprintStore(plan *compiled, component *ensured, profile string) *versionStore {
	render := func(revision int) *client.BlueprintDocument {
		return plan.blueprint(revision, component.id, component.revision, profile)
	}

	toRef := func(version *client.BlueprintVersion, err error) (versionRef, error) {
		if err != nil {
			return versionRef{}, err
		}

		if version == nil {
			return versionRef{}, errNoRevision
		}

		return versionRef{
			id: version.BlueprintID, revision: version.Revision, status: version.Status,
			document: version.Document, rowVersion: version.RowVersion,
		}, nil
	}

	return &versionStore{
		resource:          DocumentBlueprint,
		slug:              plan.slug,
		conflictExtension: client.ExtBlueprintID,
		source:            plan.blueprintSource,
		desired:           func(revision int) any { return render(revision) },
		find: func(ctx context.Context) (string, error) {
			return r.findBlueprint(ctx, plan.slug)
		},
		create: func(ctx context.Context, revision int) (versionRef, error) {
			return toRef(r.api.CreateBlueprint(ctx, r.orgID, plan.slug, render(revision)))
		},
		latest: func(ctx context.Context, id string) (*client.VersionSummary, error) {
			return r.api.LatestBlueprintRevision(ctx, id)
		},
		get: func(ctx context.Context, id string, revision int) (versionRef, error) {
			return toRef(r.api.GetBlueprintVersion(ctx, id, revision))
		},
		createVersion: func(ctx context.Context, id string, revision int) (versionRef, error) {
			return toRef(r.api.CreateBlueprintVersion(ctx, id, render(revision)))
		},
		replace: func(ctx context.Context, id string, revision, rowVersion int) (versionRef, error) {
			return toRef(r.api.ReplaceBlueprintVersion(ctx, id, revision, render(revision), &rowVersion))
		},
		publish: func(ctx context.Context, id string, revision int) (versionRef, error) {
			return toRef(r.api.PublishBlueprintVersion(ctx, id, revision))
		},
	}
}

// findBlueprint returns the id of the blueprint holding slug, or "".
func (r *deployRun) findBlueprint(ctx context.Context, slug string) (string, error) {
	found, err := r.api.ListBlueprintsBySlug(ctx, r.orgID, slug)
	if err != nil {
		return "", repoerrors.Errorf("list blueprints by slug: %w", err)
	}

	if len(found) == 0 {
		return "", nil
	}

	return found[0].ID, nil
}

// revisionNote renders the step note for a converged lineage.
func revisionNote(slug string, result *ensured) string {
	note := slug + " revision " + strconv.Itoa(result.revision)
	if !result.changed {
		note += " (unchanged)"
	}

	return note
}
