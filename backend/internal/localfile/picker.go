package localfile

import "context"

type Picker interface {
	Pick(ctx context.Context) (PickedFile, error)
}

type PickerFunc func(ctx context.Context) (PickedFile, error)

func (f PickerFunc) Pick(ctx context.Context) (PickedFile, error) {
	return f(ctx)
}
