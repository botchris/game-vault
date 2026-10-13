package rpc

import (
	"errors"
	"slices"

	"connectrpc.com/connect"

	"gamevault/internal/domain/field"
	"gamevault/internal/domain/game"
	pb "gamevault/internal/gen/gamevault/v1"
)

// fieldValuesToPB maps custom field values onto the API.
func fieldValuesToPB(values game.FieldValues) map[string]*pb.FieldValue {
	if len(values) == 0 {
		return nil
	}

	out := make(map[string]*pb.FieldValue, len(values))

	for id, v := range values {
		if pv, ok := fieldValueToPB(v); ok {
			out[id] = pv
		}
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

// fieldValueToPB maps the member that is set; ok is false for an empty value, which is never
// stored and must not reach the API as a value without a member.
func fieldValueToPB(v game.FieldValue) (_ *pb.FieldValue, ok bool) {
	switch {
	case v.Text != "":
		return &pb.FieldValue{Value: &pb.FieldValue_Text{Text: v.Text}}, true
	case v.Bool != nil:
		return &pb.FieldValue{Value: &pb.FieldValue_Bool{Bool: *v.Bool}}, true
	case v.Number != nil:
		return &pb.FieldValue{Value: &pb.FieldValue_Number{Number: *v.Number}}, true
	case v.Money != nil:
		return &pb.FieldValue{Value: &pb.FieldValue_Money{Money: &pb.Money{
			AmountMinor: v.Money.Amount,
			Currency:    v.Money.Currency,
		}}}, true
	case v.Date != "":
		return &pb.FieldValue{Value: &pb.FieldValue_Date{Date: v.Date}}, true
	case v.Minutes != nil:
		return &pb.FieldValue{Value: &pb.FieldValue_Minutes{Minutes: *v.Minutes}}, true
	case v.Choice != "":
		return &pb.FieldValue{Value: &pb.FieldValue_Choice{Choice: v.Choice}}, true
	case len(v.Choices) > 0:
		return &pb.FieldValue{Value: &pb.FieldValue_Choices{Choices: &pb.ChoiceList{Ids: slices.Clone(v.Choices)}}}, true
	default:
		return nil, false
	}
}

// fieldValuesFromPB maps the API's custom field values onto the domain; the catalog validates them.
func fieldValuesFromPB(values map[string]*pb.FieldValue) game.FieldValues {
	if len(values) == 0 {
		return nil
	}

	out := make(game.FieldValues, len(values))

	for id, v := range values {
		out[id] = fieldValueFromPB(v)
	}

	return out
}

// fieldValueFromPB copies the request's value: the domain value must not point into the message.
func fieldValueFromPB(v *pb.FieldValue) game.FieldValue {
	switch x := v.GetValue().(type) {
	case *pb.FieldValue_Text:
		return game.FieldValue{Text: x.Text}
	case *pb.FieldValue_Bool:
		b := x.Bool

		return game.FieldValue{Bool: &b}
	case *pb.FieldValue_Number:
		n := x.Number

		return game.FieldValue{Number: &n}
	case *pb.FieldValue_Money:
		return game.FieldValue{Money: &game.Money{
			Amount:   x.Money.GetAmountMinor(),
			Currency: x.Money.GetCurrency(),
		}}
	case *pb.FieldValue_Date:
		return game.FieldValue{Date: x.Date}
	case *pb.FieldValue_Minutes:
		m := x.Minutes

		return game.FieldValue{Minutes: &m}
	case *pb.FieldValue_Choice:
		return game.FieldValue{Choice: x.Choice}
	case *pb.FieldValue_Choices:
		return game.FieldValue{Choices: slices.Clone(x.Choices.GetIds())}
	default:
		return game.FieldValue{}
	}
}

func definitionToPB(d field.Definition) *pb.FieldDefinition {
	out := &pb.FieldDefinition{
		Id:       d.ID,
		Name:     d.Name,
		Type:     string(d.Type),
		Scope:    string(d.Scope),
		Decimals: int32(d.Decimals),
		Unit:     d.Unit,
		Currency: d.Currency,
	}

	for _, k := range d.Kinds {
		out.Kinds = append(out.Kinds, kindToPB[k])
	}

	for _, c := range d.Choices {
		out.Choices = append(out.Choices, choiceToPB(c))
	}

	return out
}

func choiceToPB(c field.Choice) *pb.FieldChoice {
	return &pb.FieldChoice{
		Id:   c.ID,
		Name: c.Name,
	}
}

func definitionFromPB(d *pb.FieldDefinition) (field.Definition, error) {
	out := field.Definition{
		ID:       d.GetId(),
		Name:     d.GetName(),
		Type:     field.Type(d.GetType()),
		Scope:    field.Scope(d.GetScope()),
		Decimals: int(d.GetDecimals()),
		Unit:     d.GetUnit(),
		Currency: d.GetCurrency(),
	}

	for _, k := range d.GetKinds() {
		kind, ok := kindFromPB[k]
		if !ok {
			return field.Definition{}, connect.NewError(connect.CodeInvalidArgument, errors.New("the copy kind is not valid: pick one from the list and try again"))
		}

		out.Kinds = append(out.Kinds, kind)
	}

	for _, c := range d.GetChoices() {
		out.Choices = append(out.Choices, field.Choice{
			ID:   c.GetId(),
			Name: c.GetName(),
		})
	}

	return out, nil
}
