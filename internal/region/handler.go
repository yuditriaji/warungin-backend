package region

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

const baseURL = "https://emsifa.github.io/api-wilayah-indonesia/api"

type Handler struct {
	client *http.Client
}

func NewHandler() *Handler {
	return &Handler{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

type Region struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// GetProvinces returns all Indonesian provinces
func (h *Handler) GetProvinces(c *gin.Context) {
	resp, err := h.client.Get(baseURL + "/provinces.json")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch provinces"})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	
	var provinces []Region
	if err := json.Unmarshal(body, &provinces); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse provinces"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": provinces})
}

// GetCities returns cities/regencies for a province
func (h *Handler) GetCities(c *gin.Context) {
	provinceID := c.Param("province_id")
	
	resp, err := h.client.Get(baseURL + "/regencies/" + provinceID + ".json")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch cities"})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	
	var cities []Region
	if err := json.Unmarshal(body, &cities); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse cities"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": cities})
}

// GetDistricts returns districts for a city (useful for postal code lookup)
func (h *Handler) GetDistricts(c *gin.Context) {
	cityID := c.Param("city_id")
	
	resp, err := h.client.Get(baseURL + "/districts/" + cityID + ".json")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch districts"})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	
	var districts []Region
	if err := json.Unmarshal(body, &districts); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to parse districts"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": districts})
}
