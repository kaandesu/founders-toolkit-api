package scanmanager

import (
	"founders-toolkit-api/internal/database"
	"founders-toolkit-api/internal/response"
	"founders-toolkit-api/models"
	"net/http"

	"github.com/gin-gonic/gin"
)

// GET /site/:id/scans  list scans for a single site
func ListScansForSite(db *database.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		uRaw, _ := c.Get("user")
		user, _ := uRaw.(models.User)

		if user.ID == 0 {
			response.Respond(c, http.StatusUnauthorized, "unauthorized", nil)
			return
		}

		siteID := c.Param("id")

		var site models.Site
		if err := db.DB.Where("id = ? AND user_id = ?", siteID, user.ID).
			First(&site).Error; err != nil || site.ID == 0 {
			response.Respond(c, http.StatusNotFound, "site not found", nil)
			return
		}

		var scans []models.Scan
		if err := db.DB.
			Where("site_id = ? AND user_id = ?", site.ID, user.ID).
			Order("created_at DESC").
			Find(&scans).Error; err != nil {
			response.Respond(c, http.StatusInternalServerError, "failed to load scans", nil)
			return
		}

		response.Respond(c, http.StatusOK, "Scans loaded", scans)
	}
}

///////////////////////////////////////////////////////////

// Returns a single scan by id (also checks it belongs to the current user)
func GetScan(db *database.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		uRaw, _ := c.Get("user")
		user, _ := uRaw.(models.User)
		if user.ID == 0 {
			response.Respond(c, http.StatusUnauthorized, "unauthorized", nil)
			return
		}

		id := c.Param("id")
		var scan models.Scan
		if err := db.DB.
			Table("scans").
			Where("id = ? AND user_id = ?", id, user.ID).
			First(&scan).Error; err != nil || scan.ID == 0 {
			response.Respond(c, http.StatusNotFound, "Scan not found", nil)
			return
		}

		response.Respond(c, http.StatusOK, "Scan loaded", scan)
	}
}
