package service

import (
	"sort"
	"time"

	"github.com/1260124186-cc/solo-0016-cellar-run-control/internal/domain"
)

func (s *Service) CreateFormula(input CreateFormulaInput) (domain.Formula, error) {
	now := s.clock.Now()
	formula := domain.Formula{
		Name:                  input.Name,
		Style:                 input.Style,
		TargetOriginalGravity: input.TargetOriginalGravity,
		TargetFinalGravity:    input.TargetFinalGravity,
		MaxFermentationTempC:  input.MaxFermentationTempC,
		MinimumDays:           input.MinimumDays,
		MaximumDays:           input.MaximumDays,
		Steps:                 []domain.FormulaStep{},
		State:                 domain.FormulaDraft,
		Revision:              1,
		Version:               1,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	formula.Normalize()
	if err := formula.Validate(); err != nil {
		return domain.Formula{}, err
	}
	id, err := generateEntityID("formula")
	if err != nil {
		return domain.Formula{}, domain.NewError(domain.CodeStorage, err.Error())
	}
	formula.ID = id
	nameKey := domain.NameKey(formula.Name)

	err = s.store.Execute(func(snapshot *domain.Snapshot) error {
		if _, exists := snapshot.NameIndex[nameKey]; exists {
			return domain.NewError(domain.CodeFormulaNameConflict,
				"a formula with the same normalized name already exists")
		}
		snapshot.Formulas[formula.ID] = formula
		snapshot.NameIndex[nameKey] = formula.ID
		return nil
	})
	if err != nil {
		return domain.Formula{}, err
	}
	return formula.Clone(), nil
}

func (s *Service) CreateFormulaRevision(
	sourceID string,
	input VersionInput,
) (domain.Formula, error) {
	var created domain.Formula
	err := s.store.Execute(func(snapshot *domain.Snapshot) error {
		source, ok := snapshot.Formulas[sourceID]
		if !ok {
			return domain.NewError(domain.CodeFormulaNotFound, "formula was not found")
		}
		if err := s.checkExpected(source.Version, input.ExpectedVersion); err != nil {
			return err
		}
		if source.State != domain.FormulaApproved {
			return domain.NewError(domain.CodeFormulaState,
				"only the current approved formula can start a revision")
		}
		id, err := generateEntityID("formula")
		if err != nil {
			return domain.NewError(domain.CodeStorage, err.Error())
		}
		now := s.clock.Now()
		sourceVersion := source.Version
		candidate := domain.Formula{
			ID:                    id,
			Name:                  source.Name,
			Style:                 source.Style,
			TargetOriginalGravity: source.TargetOriginalGravity,
			TargetFinalGravity:    source.TargetFinalGravity,
			MaxFermentationTempC:  source.MaxFermentationTempC,
			MinimumDays:           source.MinimumDays,
			MaximumDays:           source.MaximumDays,
			Steps:                 append([]domain.FormulaStep(nil), source.Steps...),
			State:                 domain.FormulaDraft,
			Revision:              source.Revision + 1,
			RevisionOf:            &source.ID,
			SourceVersion:         &sourceVersion,
			Version:               1,
			CreatedAt:             now,
			UpdatedAt:             now,
		}
		if err := candidate.Validate(); err != nil {
			return err
		}
		snapshot.Formulas[candidate.ID] = candidate
		created = candidate.Clone()
		return nil
	})
	if err != nil {
		return domain.Formula{}, err
	}
	return created, nil
}

func (s *Service) SetFormulaSteps(
	id string,
	input SetFormulaStepsInput,
) (domain.Formula, error) {
	var updated domain.Formula
	err := s.store.Execute(func(snapshot *domain.Snapshot) error {
		formula, ok := snapshot.Formulas[id]
		if !ok {
			return domain.NewError(domain.CodeFormulaNotFound, "formula was not found")
		}
		if err := s.checkExpected(formula.Version, input.ExpectedVersion); err != nil {
			return err
		}
		if err := formula.SetSteps(input.Steps); err != nil {
			return err
		}
		formula.UpdatedAt = s.clock.Now()
		snapshot.Formulas[id] = formula
		updated = formula.Clone()
		return nil
	})
	if err != nil {
		return domain.Formula{}, err
	}
	return updated, nil
}

func (s *Service) ApproveFormula(id string, input VersionInput) (domain.Formula, error) {
	var updated domain.Formula
	err := s.store.Execute(func(snapshot *domain.Snapshot) error {
		formula, ok := snapshot.Formulas[id]
		if !ok {
			return domain.NewError(domain.CodeFormulaNotFound, "formula was not found")
		}
		if err := s.checkExpected(formula.Version, input.ExpectedVersion); err != nil {
			return err
		}
		now := s.clock.Now()
		if err := formula.Approve(now); err != nil {
			return err
		}
		if formula.RevisionOf != nil {
			source, ok := snapshot.Formulas[*formula.RevisionOf]
			if !ok {
				return domain.NewError(domain.CodeFormulaNotFound,
					"revision source formula was not found")
			}
			if formula.SourceVersion == nil || source.Version != *formula.SourceVersion {
				return domain.NewError(domain.CodeRevisionConflict,
					"the revision source has changed since this candidate was created")
			}
			if err := source.Supersede(formula.ID, now); err != nil {
				return err
			}
			snapshot.Formulas[source.ID] = source
		}
		snapshot.Formulas[id] = formula
		updated = formula.Clone()
		return nil
	})
	if err != nil {
		return domain.Formula{}, err
	}
	return updated, nil
}

func (s *Service) RetireFormula(id string, input VersionInput) (domain.Formula, error) {
	var updated domain.Formula
	err := s.store.Execute(func(snapshot *domain.Snapshot) error {
		formula, ok := snapshot.Formulas[id]
		if !ok {
			return domain.NewError(domain.CodeFormulaNotFound, "formula was not found")
		}
		if err := s.checkExpected(formula.Version, input.ExpectedVersion); err != nil {
			return err
		}
		for _, run := range snapshot.Runs {
			if run.FormulaID == id && run.State != domain.RunCompleted &&
				run.State != domain.RunAborted {
				return domain.NewError(domain.CodeFormulaInUse,
					"formula is attached to a run that is still open")
			}
		}
		if err := formula.Retire(s.clock.Now()); err != nil {
			return err
		}
		snapshot.Formulas[id] = formula
		updated = formula.Clone()
		return nil
	})
	if err != nil {
		return domain.Formula{}, err
	}
	return updated, nil
}

func (s *Service) GetFormula(id string) (domain.Formula, error) {
	var found domain.Formula
	err := s.store.View(func(snapshot domain.Snapshot) error {
		formula, ok := snapshot.Formulas[id]
		if !ok {
			return domain.NewError(domain.CodeFormulaNotFound, "formula was not found")
		}
		found = formula.Clone()
		return nil
	})
	if err != nil {
		return domain.Formula{}, err
	}
	return found, nil
}

func (s *Service) ListFormulas() ([]domain.Formula, error) {
	var formulas []domain.Formula
	err := s.store.View(func(snapshot domain.Snapshot) error {
		formulas = make([]domain.Formula, 0, len(snapshot.Formulas))
		for _, formula := range snapshot.Formulas {
			formulas = append(formulas, formula.Clone())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(formulas, func(i, j int) bool {
		if formulas[i].Name == formulas[j].Name {
			return formulas[i].ID < formulas[j].ID
		}
		return formulas[i].Name < formulas[j].Name
	})
	return formulas, nil
}

func timestampPointer(value time.Time) *time.Time {
	copy := value.UTC()
	return &copy
}
