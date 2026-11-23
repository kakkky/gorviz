package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/kakkky/gorviz/example/todo-app/domain"
	"github.com/kakkky/gorviz/example/todo-app/testhelper/mock"
	"github.com/kakkky/gorviz/example/todo-app/usecase"
)

func TestAddTodoUsecase_Exec(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	wantErr := errors.New("boom")
	withReminder := domain.Todo{ID: 1, Title: "牛乳を買う", Reminder: true}

	tests := []struct {
		name    string
		input   usecase.AddTodoUsecaseInput
		mock    func(todos *mock.MockTodoAccessor, notifier *mock.MockNotifierClient)
		want    usecase.AddTodoUsecaseOutput
		wantErr error
	}{
		{
			name:  "adds a todo without a reminder and does not call the notifier",
			input: usecase.AddTodoUsecaseInput{Title: "本を返す"},
			mock: func(todos *mock.MockTodoAccessor, notifier *mock.MockNotifierClient) {
				todos.EXPECT().Add(ctx, domain.Title("本を返す"), false, gomock.Any()).Return(domain.Todo{ID: 2, Title: "本を返す"}, nil)
			},
			want: usecase.AddTodoUsecaseOutput{Todo: domain.Todo{ID: 2, Title: "本を返す"}},
		},
		{
			name:  "keeps the todo when the reminder times out",
			input: usecase.AddTodoUsecaseInput{Title: "牛乳を買う", Reminder: true},
			mock: func(todos *mock.MockTodoAccessor, notifier *mock.MockNotifierClient) {
				todos.EXPECT().Add(ctx, domain.Title("牛乳を買う"), true, gomock.Any()).Return(withReminder, nil)
				notifier.EXPECT().RegisterReminder(ctx, withReminder).Return(wantErr)
			},
			want: usecase.AddTodoUsecaseOutput{Todo: withReminder},
		},
		{
			name:  "returns the add error",
			input: usecase.AddTodoUsecaseInput{Title: "牛乳を買う"},
			mock: func(todos *mock.MockTodoAccessor, notifier *mock.MockNotifierClient) {
				todos.EXPECT().Add(ctx, domain.Title("牛乳を買う"), false, gomock.Any()).Return(domain.Todo{}, wantErr)
			},
			wantErr: wantErr,
		},
		{
			name:    "rejects an empty title without adding",
			input:   usecase.AddTodoUsecaseInput{Title: " "},
			mock:    func(todos *mock.MockTodoAccessor, notifier *mock.MockNotifierClient) {},
			wantErr: domain.ErrInvalidArgument,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			todos := mock.NewMockTodoAccessor(ctrl)
			notifier := mock.NewMockNotifierClient(ctrl)
			tt.mock(todos, notifier)

			uc := usecase.NewUseCase(todos, nil, notifier, nil).NewAddTodoUsecase()
			out, err := uc.Exec(ctx, tt.input)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, usecase.AddTodoUsecaseOutput{}, out)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, out)
		})
	}
}
