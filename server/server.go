package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	"github-activity-tracker/DB"
	"github-activity-tracker/models"
)

func fetchPRsFromGitHub(username string, month string) ([]models.PR, error) {
	url := fmt.Sprintf("https://api.github.com/search/issues?q=author:%s+type:pr+created:%s", username, month)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Items []struct {
			Title   string `json:"title"`
			State   string `json:"state"`
			HtmlURL string `json:"html_url"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var prs []models.PR
	for _, item := range result.Items {
		prs = append(prs, models.PR{
			Title:  item.Title,
			Status: item.State,
			URL:    item.HtmlURL,
			Merged: item.State == "closed", // Approximate - would need more API calls to determine if actually merged
		})
	}
	return prs, nil
}

func main() {
	DB.InitDB()
	db := DB.GetDB()

	// Get port from environment or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Create Gin router
	r := gin.Default()

	r.GET("/greet", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "Hello World!"})
	})

	r.POST("/users", func(c *gin.Context) {
		var user models.User
		if err := c.ShouldBindJSON(&user); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		db.Create(&user)

		months := []string{"2025-09", "2025-10"}
		for _, monthName := range months {
			prs, err := fetchPRsFromGitHub(user.GithubUser, monthName)
			if err == nil {
				var month models.Month
				db.Where("name = ?", monthName).FirstOrCreate(&month, models.Month{Name: monthName})
				for _, pr := range prs {
					pr.UserID = user.ID
					pr.MonthID = month.ID
					db.Create(&pr)
				}
			}
		}
		c.JSON(http.StatusCreated, user)
	})

	r.GET("/leaderboard", func(c *gin.Context) {
		var users []models.User
		db.Preload("PRs.Month").Find(&users)

		var leaderboard []map[string]interface{}
		for _, user := range users {
			prCount := 0
			for _, pr := range user.PRs {
				if pr.Month.Name == "2025-09" || pr.Month.Name == "2025-10" {
					prCount++
				}
			}
			leaderboard = append(leaderboard, map[string]interface{}{
				"name":        user.Name,
				"github_user": user.GithubUser,
				"pr_count":    prCount,
			})
		}
		c.JSON(http.StatusOK, leaderboard)
	})

	r.GET("/admin-dashboard", func(c *gin.Context) {
		var users []models.User
		db.Preload("PRs.Month").Find(&users)

		var dashboard []map[string]interface{}
		for _, user := range users {
			var prs []models.PR
			for _, pr := range user.PRs {
				if pr.Month.Name == "2025-09" || pr.Month.Name == "2025-10" {
					prs = append(prs, pr)
				}
			}
			dashboard = append(dashboard, map[string]interface{}{
				"name":        user.Name,
				"github_user": user.GithubUser,
				"prs":         prs,
			})
		}
		c.JSON(http.StatusOK, dashboard)
	})

	r.POST("/track-prs", func(c *gin.Context) {
		var req struct {
			Usernames []string `json:"usernames"`
			MonthName string   `json:"month_name"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var results []map[string]interface{}
		for _, username := range req.Usernames {
			var user models.User
			if err := db.Where("github_user = ?", username).First(&user).Error; err != nil {
				results = append(results, map[string]interface{}{
					"username": username,
					"prs":      nil,
				})
				continue
			}

			var month models.Month
			if err := db.Where("name = ?", req.MonthName).First(&month).Error; err != nil {
				results = append(results, map[string]interface{}{
					"username": username,
					"prs":      nil,
				})
				continue
			}

			var prs []models.PR
			if err := db.Preload("User").Preload("Org").Preload("Project").Preload("Month").
				Where("user_id = ? AND month_id = ?", user.ID, month.ID).Find(&prs).Error; err != nil {
				results = append(results, map[string]interface{}{
					"username": username,
					"prs":      nil,
				})
				continue
			}

			var prList []map[string]interface{}
			for _, pr := range prs {
				prList = append(prList, map[string]interface{}{
					"id":      pr.ID,
					"title":   pr.Title,
					"url":     pr.URL,
					"status":  pr.Status,
					"merged":  pr.Merged,
					"created": pr.CreatedAt,
					"org":     pr.Org.Name,
					"project": pr.Project.Name,
					"month":   pr.Month.Name,
				})
			}

			results = append(results, map[string]interface{}{
				"username": username,
				"prs":      prList,
			})
		}
		c.JSON(http.StatusOK, results)
	})

	fmt.Printf("Server starting on port %s\n", port)
	r.Run(":" + port)
}