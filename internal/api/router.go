package api

import (
	"net/http"

	"doc-manager/internal/api/handler"
	"doc-manager/internal/api/middleware"
	"doc-manager/internal/service"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

type RouterParams struct {
	AuthService *service.AuthService
	DocService  *service.DocumentService
}

func SetupRouter(params RouterParams) http.Handler {
	r := chi.NewRouter()

	// Base middlewares
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(middleware.SecurityHeaders)

	// CORS configuration
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link", "Content-Disposition"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Handlers initialization
	authHandler := handler.NewAuthHandler(params.AuthService)
	docHandler := handler.NewDocumentHandler(params.DocService)
	publicHandler := handler.NewPublicHandler(params.DocService)
	trashHandler := handler.NewTrashHandler(params.DocService)

	authMiddleware := middleware.AuthMiddleware(params.AuthService)

	r.Route("/api/v1", func(api chi.Router) {
		// 6.1 Autenticación y Control de Acceso
		api.Route("/auth", func(auth chi.Router) {
			auth.Post("/register", authHandler.Register)
			auth.Post("/login", authHandler.Login)
			auth.Post("/qr/init", authHandler.InitQR)
			auth.Get("/qr/status/{ticket}", authHandler.GetQRStatus)

			// Protected QR authorization
			auth.With(authMiddleware).Post("/qr/authorize", authHandler.AuthorizeQR)
		})

		// 6.2 Gestión de Documentos (Protegido)
		api.Route("/documents", func(docs chi.Router) {
			docs.Use(authMiddleware)

			docs.Post("/", docHandler.Upload)
			docs.Get("/", docHandler.List)
			docs.Get("/{id}/metadata", docHandler.GetMetadata)
			docs.Patch("/{id}/visibility", docHandler.UpdateVisibility)
			docs.Get("/{id}/download", docHandler.DownloadProtected)
			docs.Post("/{id}/share", docHandler.Share)
			docs.Delete("/{id}", docHandler.SoftDelete)
		})

		// 6.3 Enlaces y Descargas Públicas (Anónimo)
		api.Route("/public/documents", func(pub chi.Router) {
			pub.Get("/{id}/download", publicHandler.DownloadPublic)
			pub.Get("/signed-download", publicHandler.DownloadSigned)
		})

		// 6.4 Papelera de Reciclaje (Protegido)
		api.Route("/trash", func(trash chi.Router) {
			trash.Use(authMiddleware)

			trash.Get("/", trashHandler.List)
			trash.Post("/{id}/restore", trashHandler.Restore)
			trash.Delete("/{id}", trashHandler.HardDelete)
		})
	})

	return r
}
