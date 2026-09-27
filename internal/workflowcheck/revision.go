package workflowcheck

import (
	"fmt"

	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/domain"
)

func formulaRevision(api *client) error {
	source, err := ensureApprovedFormula(api, "Revision Pale")
	if err != nil {
		return err
	}
	if source.RevisionNumber != 1 || source.LineageID == "" {
		return fmt.Errorf("approved formula did not open a lineage at revision 1")
	}

	firstRunResponse, err := api.call("POST", "/v1/runs",
		createRunPayload("RUN-REVISION-A", source.ID), nil)
	if err != nil {
		return err
	}
	if err := requireStatus(firstRunResponse, 201, "create run on first revision"); err != nil {
		return err
	}
	var firstRun runView
	if err := firstRunResponse.decode(&firstRun); err != nil {
		return err
	}
	if firstRun.FormulaID != source.ID || firstRun.FormulaRevision != 1 {
		return fmt.Errorf("run did not record the adopted formula revision")
	}

	reviseResponse, err := api.call("POST", "/v1/formulas/"+source.ID+"/revise", nil, nil)
	if err != nil {
		return err
	}
	if err := requireStatus(reviseResponse, 201, "revise approved formula"); err != nil {
		return err
	}
	var candidate formulaView
	if err := reviseResponse.decode(&candidate); err != nil {
		return err
	}
	if candidate.State != domain.FormulaDraft || candidate.Version != 1 ||
		candidate.RevisionNumber != 2 || candidate.SourceFormulaID != source.ID ||
		candidate.SourceRevision != 1 || candidate.LineageID != source.LineageID ||
		len(candidate.Steps) != 3 {
		return fmt.Errorf("revision candidate did not record its source version")
	}

	currentResponse, err := api.call("GET", "/v1/formulas/"+source.ID, nil, nil)
	if err != nil {
		return err
	}
	if err := requireStatus(currentResponse, 200, "read source during revision"); err != nil {
		return err
	}
	var current formulaView
	if err := currentResponse.decode(&current); err != nil {
		return err
	}
	if current.State != domain.FormulaApproved {
		return fmt.Errorf("revision candidate replaced the current version before approval")
	}

	secondRunResponse, err := api.call("POST", "/v1/runs",
		createRunPayload("RUN-REVISION-B", source.ID), nil)
	if err != nil {
		return err
	}
	if err := requireStatus(secondRunResponse, 201, "run still uses current revision"); err != nil {
		return err
	}
	var secondRun runView
	if err := secondRunResponse.decode(&secondRun); err != nil {
		return err
	}
	if secondRun.FormulaRevision != 1 {
		return fmt.Errorf("pending revision changed the runnable version")
	}

	draftSource, err := api.call("POST", "/v1/formulas/"+candidate.ID+"/revise", nil, nil)
	if err != nil {
		return err
	}
	if err := requireCode(draftSource, 409, string(domain.CodeFormulaRevisionSource),
		"revise a non-approved source"); err != nil {
		return err
	}

	missingSource, err := api.call("POST", "/v1/formulas/formula_missing/revise", nil, nil)
	if err != nil {
		return err
	}
	if err := requireCode(missingSource, 404, string(domain.CodeFormulaNotFound),
		"revise a missing source"); err != nil {
		return err
	}

	competingResponse, err := api.call("POST", "/v1/formulas/"+source.ID+"/revise", nil, nil)
	if err != nil {
		return err
	}
	if err := requireStatus(competingResponse, 201, "revise current source again"); err != nil {
		return err
	}
	var competing formulaView
	if err := competingResponse.decode(&competing); err != nil {
		return err
	}
	if competing.RevisionNumber != 2 || competing.SourceFormulaID != source.ID {
		return fmt.Errorf("competing candidate did not record its source version")
	}

	stepsResponse, err := api.call("PUT", "/v1/formulas/"+candidate.ID+"/steps",
		formulaStepPayload(), ptrInt64(1))
	if err != nil {
		return err
	}
	if err := requireStatus(stepsResponse, 200, "set revision candidate steps"); err != nil {
		return err
	}
	if err := stepsResponse.decode(&candidate); err != nil {
		return err
	}
	if candidate.Version != 2 {
		return fmt.Errorf("revision candidate steps were not accepted as one revision")
	}

	approvedResponse, err := api.call("POST", "/v1/formulas/"+candidate.ID+"/approve",
		nil, ptrInt64(2))
	if err != nil {
		return err
	}
	if err := requireStatus(approvedResponse, 200, "approve revision candidate"); err != nil {
		return err
	}
	if err := approvedResponse.decode(&candidate); err != nil {
		return err
	}
	if candidate.State != domain.FormulaApproved || candidate.RevisionNumber != 2 {
		return fmt.Errorf("approved revision has unexpected state or revision number")
	}

	supersededResponse, err := api.call("GET", "/v1/formulas/"+source.ID, nil, nil)
	if err != nil {
		return err
	}
	if err := requireStatus(supersededResponse, 200, "read superseded formula"); err != nil {
		return err
	}
	var superseded formulaView
	if err := supersededResponse.decode(&superseded); err != nil {
		return err
	}
	if superseded.State != domain.FormulaSuperseded {
		return fmt.Errorf("source formula was not superseded by the approved revision")
	}

	editOld, err := api.call("PUT", "/v1/formulas/"+source.ID+"/steps",
		formulaStepPayload(), ptrInt64(superseded.Version))
	if err != nil {
		return err
	}
	if err := requireCode(editOld, 409, string(domain.CodeFormulaState),
		"edit superseded formula"); err != nil {
		return err
	}

	runOnOld, err := api.call("POST", "/v1/runs",
		createRunPayload("RUN-REVISION-C", source.ID), nil)
	if err != nil {
		return err
	}
	if err := requireCode(runOnOld, 409, string(domain.CodeFormulaState),
		"run on superseded formula"); err != nil {
		return err
	}

	repeated, err := api.call("POST", "/v1/formulas/"+candidate.ID+"/approve",
		nil, ptrInt64(candidate.Version))
	if err != nil {
		return err
	}
	if err := requireCode(repeated, 409, string(domain.CodeFormulaState),
		"approve revision twice"); err != nil {
		return err
	}

	stale, err := api.call("POST", "/v1/formulas/"+competing.ID+"/approve",
		nil, ptrInt64(competing.Version))
	if err != nil {
		return err
	}
	if err := requireCode(stale, 409, string(domain.CodeFormulaRevisionConflict),
		"approve stale revision candidate"); err != nil {
		return err
	}

	competingRead, err := api.call("GET", "/v1/formulas/"+competing.ID, nil, nil)
	if err != nil {
		return err
	}
	if err := requireStatus(competingRead, 200, "read stale candidate"); err != nil {
		return err
	}
	if err := competingRead.decode(&competing); err != nil {
		return err
	}
	if competing.State != domain.FormulaDraft {
		return fmt.Errorf("rejected stale approval left a half-approved formula")
	}

	thirdRunResponse, err := api.call("POST", "/v1/runs",
		createRunPayload("RUN-REVISION-D", candidate.ID), nil)
	if err != nil {
		return err
	}
	if err := requireStatus(thirdRunResponse, 201, "run on approved revision"); err != nil {
		return err
	}
	var thirdRun runView
	if err := thirdRunResponse.decode(&thirdRun); err != nil {
		return err
	}
	if thirdRun.FormulaID != candidate.ID || thirdRun.FormulaRevision != 2 {
		return fmt.Errorf("run did not trace to the newly approved revision")
	}

	revisionsResponse, err := api.call("GET", "/v1/formulas/"+source.ID+"/revisions", nil, nil)
	if err != nil {
		return err
	}
	if err := requireStatus(revisionsResponse, 200, "list lineage revisions"); err != nil {
		return err
	}
	var history struct {
		Revisions []formulaView `json:"revisions"`
	}
	if err := revisionsResponse.decode(&history); err != nil {
		return err
	}
	if len(history.Revisions) != 3 || history.Revisions[0].RevisionNumber != 1 {
		return fmt.Errorf("lineage history was not queryable after supersession")
	}
	for _, member := range history.Revisions {
		if member.LineageID != source.LineageID {
			return fmt.Errorf("lineage history mixes different lineages")
		}
	}

	firstRunRead, err := api.call("GET", "/v1/runs/"+firstRun.ID, nil, nil)
	if err != nil {
		return err
	}
	if err := requireStatus(firstRunRead, 200, "read run on superseded revision"); err != nil {
		return err
	}
	if err := firstRunRead.decode(&firstRun); err != nil {
		return err
	}
	if firstRun.FormulaID != source.ID || firstRun.FormulaRevision != 1 {
		return fmt.Errorf("run lost trace of the superseded formula revision")
	}
	return nil
}
