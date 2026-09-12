package handler

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/gbschedule/gbschedule/internal/dto"
	"github.com/gbschedule/gbschedule/internal/service"
)

// ScheduleVersionHandler handles timetable snapshot HTTP endpoints.
type ScheduleVersionHandler struct {
	service service.ScheduleVersionService
	logger  *slog.Logger
}

// NewScheduleVersionHandler constructs a schedule version handler.
func NewScheduleVersionHandler(service service.ScheduleVersionService, logger *slog.Logger) *ScheduleVersionHandler {
	return &ScheduleVersionHandler{service: service, logger: logger}
}

// List godoc
// @Summary List schedule versions
// @Tags schedule-versions
// @Produce json
// @Param page query int false "page"
// @Param page_size query int false "page size"
// @Success 200 {object} dto.Response
// @Router /api/v1/schedule-versions [get]
func (h *ScheduleVersionHandler) List(c *gin.Context) {
	var p dto.Pagination
	if err := c.ShouldBindQuery(&p); err != nil {
		BadRequest(c, "invalid pagination")
		return
	}
	p.Normalize()
	items, total, err := h.service.List(c.Request.Context(), p.Page, p.PageSize)
	if err != nil {
		Error(c, err)
		return
	}
	OK(c, dto.PageData{Items: items, Total: total, Page: p.Page, PageSize: p.PageSize})
}

// Get godoc
// @Summary Get one schedule version with its snapshot entries
// @Tags schedule-versions
// @Produce json
// @Param id path int true "version id"
// @Success 200 {object} dto.Response
// @Router /api/v1/schedule-versions/{id} [get]
func (h *ScheduleVersionHandler) Get(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	item, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		Error(c, err)
		return
	}
	OK(c, item)
}

// Compare godoc
// @Summary Compare two schedule versions
// @Tags schedule-versions
// @Produce json
// @Param from query int true "source version id"
// @Param to query int true "target version id"
// @Success 200 {object} dto.Response
// @Router /api/v1/schedule-versions/compare [get]
func (h *ScheduleVersionHandler) Compare(c *gin.Context) {
	var req dto.CompareScheduleVersionsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		BadRequest(c, err.Error())
		return
	}
	result, err := h.service.Compare(c.Request.Context(), req.From, req.To)
	if err != nil {
		Error(c, err)
		return
	}
	OK(c, result)
}

// Publish godoc
// @Summary Publish a schedule version
// @Tags schedule-versions
// @Produce json
// @Param id path int true "version id"
// @Success 200 {object} dto.Response
// @Router /api/v1/schedule-versions/{id}/publish [post]
func (h *ScheduleVersionHandler) Publish(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	item, err := h.service.Publish(c.Request.Context(), id)
	if err != nil {
		Error(c, err)
		return
	}
	OK(c, item)
}
