package store

import "github.com/norhua/sonexa/internal/provider"

// RememberCandidateArtwork 仅登记来源已解析的目录，不下载、不创建发行。
func (s *Store) RememberCandidateArtwork(c provider.Candidate) error {
	input := c.ArtworkCatalog
	if input == nil {
		return nil
	}
	catalog, err := s.GetCoverCatalog(input.Key)
	if err != nil || (catalog != nil && catalog.State == "found") {
		return err
	}
	_, err = s.SaveCoverCatalog(input.Key, input.Images)
	return err
}
