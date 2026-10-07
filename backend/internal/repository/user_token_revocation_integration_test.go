//go:build integration

package repository

import (
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (s *UserRepoSuite) TestTokenRevocationPersistsWithoutOverwritingUserData() {
	user := s.mustCreateUser(&service.User{Email: "revoke-tokens@example.com", Balance: 10})
	stale, err := s.repo.GetByID(s.ctx, user.ID)
	s.Require().NoError(err)
	s.Require().NoError(s.repo.DeductBalance(s.ctx, user.ID, 3))

	const revocations = 8
	errs := make(chan error, revocations)
	var wg sync.WaitGroup
	for i := 0; i < revocations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.repo.IncrementTokenVersion(s.ctx, user.ID)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		s.Require().NoError(err)
	}
	stale.Username = "updated"
	s.Require().NoError(s.repo.Update(s.ctx, stale, service.UserUpdateFields{Username: true}))
	loaded, err := s.repo.GetByID(s.ctx, user.ID)
	s.Require().NoError(err)
	s.Require().Equal(int64(revocations), loaded.TokenVersion)
	s.Require().Equal(float64(7), loaded.Balance)
	s.Require().Equal("updated", loaded.Username)
}
