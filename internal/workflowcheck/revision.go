package workflowcheck

import (
	"fmt"

	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/domain"
)

func formulaRevision(api *client) error {
	createdResponse, err := api.call("POST", "/v1/formulas",
		createFormulaPayload("Revision Pale"), nil)
	if err != nil {
		return err
	}
	if err := requireStatus(createdResponse, 201, "create base formula"); err != nil {
		return err
	}
	var base formulaView
	if err := createdResponse.decode(&base); err != nil {
		return err
	}
	if base.Revision != 1 || base.RevisionOf != nil || base.SourceVersion != nil {
		return fmt.Errorf("base formula has unexpected revision metadata")
	}

	stepsResponse, err := api.call("PUT", "/v1/formulas/"+base.ID+"/steps",
		formulaStepPayload(), ptrInt64(1))
	if err != nil {
		return err
	}
	if err := requireStatus(stepsResponse, 200, "set base steps"); err != nil {
		return err
	}
	approveResponse, err := api.call("POST", "/v1/formulas/"+base.ID+"/approve",
		nil, ptrInt64(2))
	if err != nil {
		return err
	}
	if err := requireStatus(approveResponse, 200, "approve base formula"); err != nil {
		return err
	}

	staleInitiation, err := api.call("POST", "/v1/formulas/"+base.ID+"/revisions",
		nil, ptrInt64(99))
	if err != nil {
		return err
	}
	if err := requireCode(staleInitiation, 412, string(domain.CodeRevisionConflict),
		"start revision with stale expected version"); err != nil {
		return err
	}

	candidateResponse, err := api.call("POST", "/v1/formulas/"+base.ID+"/revisions",
		nil, ptrInt64(3))
	if err != nil {
		return err
	}
	if err := requireStatus(candidateResponse, 201, "start revision candidate"); err != nil {
		return err
	}
	var candidate formulaView
	if err := candidateResponse.decode(&candidate); err != nil {
		return err
	}
	if candidate.State != domain.FormulaDraft || candidate.Version != 1 ||
		candidate.Revision != 2 {
		return fmt.Errorf("revision candidate has unexpected state, version, or revision")
	}
	if candidate.RevisionOf == nil || *candidate.RevisionOf != base.ID ||
		candidate.SourceVersion == nil || *candidate.SourceVersion != 3 {
		return fmt.Errorf("revision candidate did not record its source version")
	}

	baseAfterInitiation, err := getFormula(api, base.ID)
	if err != nil {
		return err
	}
	if baseAfterInitiation.State != domain.FormulaApproved ||
		baseAfterInitiation.SupersededBy != nil {
		return fmt.Errorf("revision candidate replaced the current formula before approval")
	}

	currentRunResponse, err := api.call("POST", "/v1/runs",
		createRunPayload("RUN-REV-1", base.ID), nil)
	if err != nil {
		return err
	}
	if err := requireStatus(currentRunResponse, 201, "run against current revision"); err != nil {
		return err
	}
	var currentRun runView
	if err := currentRunResponse.decode(&currentRun); err != nil {
		return err
	}
	if currentRun.FormulaID != base.ID || currentRun.FormulaRevision != 1 {
		return fmt.Errorf("run did not record the adopted formula revision")
	}

	draftRunResponse, err := api.call("POST", "/v1/runs",
		createRunPayload("RUN-REV-DRAFT", candidate.ID), nil)
	if err != nil {
		return err
	}
	if err := requireCode(draftRunResponse, 409, string(domain.CodeFormulaState),
		"run against draft candidate"); err != nil {
		return err
	}

	candidateStepsResponse, err := api.call("PUT", "/v1/formulas/"+candidate.ID+"/steps",
		formulaRevisionStepPayload(), ptrInt64(1))
	if err != nil {
		return err
	}
	if err := requireStatus(candidateStepsResponse, 200, "set candidate steps"); err != nil {
		return err
	}
	if err := candidateStepsResponse.decode(&candidate); err != nil {
		return err
	}
	if candidate.Version != 2 || len(candidate.Steps) != 2 {
		return fmt.Errorf("revision candidate did not accept its own step list")
	}

	approveCandidate, err := api.call("POST", "/v1/formulas/"+candidate.ID+"/approve",
		nil, ptrInt64(2))
	if err != nil {
		return err
	}
	if err := requireStatus(approveCandidate, 200, "approve revision candidate"); err != nil {
		return err
	}
	if err := approveCandidate.decode(&candidate); err != nil {
		return err
	}
	if candidate.State != domain.FormulaApproved || candidate.Revision != 2 {
		return fmt.Errorf("approved candidate has unexpected state or revision")
	}

	supersededBase, err := getFormula(api, base.ID)
	if err != nil {
		return err
	}
	if supersededBase.State != domain.FormulaSuperseded ||
		supersededBase.SupersededBy == nil || *supersededBase.SupersededBy != candidate.ID {
		return fmt.Errorf("previous formula was not superseded by the approved candidate")
	}

	supersededSteps, err := api.call("PUT", "/v1/formulas/"+base.ID+"/steps",
		formulaStepPayload(), nil)
	if err != nil {
		return err
	}
	if err := requireCode(supersededSteps, 409, string(domain.CodeFormulaState),
		"edit superseded formula"); err != nil {
		return err
	}

	supersededRun, err := api.call("POST", "/v1/runs",
		createRunPayload("RUN-REV-OLD", base.ID), nil)
	if err != nil {
		return err
	}
	if err := requireCode(supersededRun, 409, string(domain.CodeFormulaState),
		"run against superseded formula"); err != nil {
		return err
	}

	repeatedApproval, err := api.call("POST", "/v1/formulas/"+candidate.ID+"/approve",
		nil, ptrInt64(3))
	if err != nil {
		return err
	}
	if err := requireCode(repeatedApproval, 409, string(domain.CodeFormulaState),
		"approve candidate twice"); err != nil {
		return err
	}

	newRunResponse, err := api.call("POST", "/v1/runs",
		createRunPayload("RUN-REV-2", candidate.ID), nil)
	if err != nil {
		return err
	}
	if err := requireStatus(newRunResponse, 201, "run against new revision"); err != nil {
		return err
	}
	var newRun runView
	if err := newRunResponse.decode(&newRun); err != nil {
		return err
	}
	if newRun.FormulaID != candidate.ID || newRun.FormulaRevision != 2 {
		return fmt.Errorf("run did not trace to the newly approved formula revision")
	}

	firstSibling, err := startRevision(api, candidate.ID)
	if err != nil {
		return err
	}
	secondSibling, err := startRevision(api, candidate.ID)
	if err != nil {
		return err
	}
	if firstSibling.SourceVersion == nil || *firstSibling.SourceVersion != 3 ||
		secondSibling.SourceVersion == nil || *secondSibling.SourceVersion != 3 {
		return fmt.Errorf("sibling candidates recorded unexpected source versions")
	}

	approveFirst, err := api.call("POST", "/v1/formulas/"+firstSibling.ID+"/approve",
		nil, ptrInt64(1))
	if err != nil {
		return err
	}
	if err := requireStatus(approveFirst, 200, "approve first sibling candidate"); err != nil {
		return err
	}

	staleApproval, err := api.call("POST", "/v1/formulas/"+secondSibling.ID+"/approve",
		nil, ptrInt64(1))
	if err != nil {
		return err
	}
	if err := requireCode(staleApproval, 412, string(domain.CodeRevisionConflict),
		"approve stale candidate"); err != nil {
		return err
	}
	staleCandidate, err := getFormula(api, secondSibling.ID)
	if err != nil {
		return err
	}
	if staleCandidate.State != domain.FormulaDraft || staleCandidate.Version != 1 {
		return fmt.Errorf("rejected stale candidate left partial state behind")
	}
	parent, err := getFormula(api, candidate.ID)
	if err != nil {
		return err
	}
	if parent.SupersededBy == nil || *parent.SupersededBy != firstSibling.ID {
		return fmt.Errorf("rejected stale candidate corrupted the supersession record")
	}

	draftSource, err := api.call("POST", "/v1/formulas/"+secondSibling.ID+"/revisions",
		nil, nil)
	if err != nil {
		return err
	}
	if err := requireCode(draftSource, 409, string(domain.CodeFormulaState),
		"revise a draft formula"); err != nil {
		return err
	}
	supersededSource, err := api.call("POST", "/v1/formulas/"+candidate.ID+"/revisions",
		nil, nil)
	if err != nil {
		return err
	}
	if err := requireCode(supersededSource, 409, string(domain.CodeFormulaState),
		"revise a superseded formula"); err != nil {
		return err
	}
	missingSource, err := api.call("POST", "/v1/formulas/formula_missing/revisions",
		nil, nil)
	if err != nil {
		return err
	}
	if err := requireCode(missingSource, 404, string(domain.CodeFormulaNotFound),
		"revise a missing formula"); err != nil {
		return err
	}

	retired, err := ensureApprovedFormula(api, "Retired Pale")
	if err != nil {
		return err
	}
	retireResponse, err := api.call("POST", "/v1/formulas/"+retired.ID+"/retire",
		nil, ptrInt64(3))
	if err != nil {
		return err
	}
	if err := requireStatus(retireResponse, 200, "retire formula"); err != nil {
		return err
	}
	retiredSource, err := api.call("POST", "/v1/formulas/"+retired.ID+"/revisions",
		nil, nil)
	if err != nil {
		return err
	}
	if err := requireCode(retiredSource, 409, string(domain.CodeFormulaState),
		"revise a retired formula"); err != nil {
		return err
	}

	duplicateName, err := api.call("POST", "/v1/formulas",
		createFormulaPayload("  revision   pale  "), nil)
	if err != nil {
		return err
	}
	if err := requireCode(duplicateName, 409, string(domain.CodeFormulaNameConflict),
		"create formula with a lineage name"); err != nil {
		return err
	}

	listResponse, err := api.call("GET", "/v1/formulas", nil, nil)
	if err != nil {
		return err
	}
	if err := requireStatus(listResponse, 200, "list formulas"); err != nil {
		return err
	}
	var list struct {
		Formulas []formulaView `json:"formulas"`
	}
	if err := listResponse.decode(&list); err != nil {
		return err
	}
	if len(list.Formulas) != 5 {
		return fmt.Errorf("expected 5 formulas after rejected writes, got %d",
			len(list.Formulas))
	}
	return nil
}

func startRevision(api *client, sourceID string) (formulaView, error) {
	response, err := api.call("POST", "/v1/formulas/"+sourceID+"/revisions", nil, nil)
	if err != nil {
		return formulaView{}, err
	}
	if err := requireStatus(response, 201, "start sibling revision candidate"); err != nil {
		return formulaView{}, err
	}
	var candidate formulaView
	if err := response.decode(&candidate); err != nil {
		return formulaView{}, err
	}
	return candidate, nil
}

func getFormula(api *client, id string) (formulaView, error) {
	response, err := api.call("GET", "/v1/formulas/"+id, nil, nil)
	if err != nil {
		return formulaView{}, err
	}
	if err := requireStatus(response, 200, "read formula"); err != nil {
		return formulaView{}, err
	}
	var formula formulaView
	if err := response.decode(&formula); err != nil {
		return formulaView{}, err
	}
	return formula, nil
}

func formulaRevisionStepPayload() map[string]any {
	return map[string]any{
		"steps": []map[string]any{
			{
				"sequence":      1,
				"label":         "Heat and combine",
				"temperature_c": 65.0,
				"hold_minutes":  45,
			},
			{
				"sequence":      2,
				"label":         "Hold for primary activity",
				"temperature_c": 18.0,
				"hold_minutes":  360,
			},
		},
	}
}
