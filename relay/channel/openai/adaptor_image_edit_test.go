package openai

import (
	"bytes"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	appcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConvertImageEditPreservesMultipartImageFieldName(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, fieldName := range []string{"image", "image[]", "image[0]"} {
		t.Run(fieldName, func(t *testing.T) {
			var input bytes.Buffer
			inputWriter := multipart.NewWriter(&input)
			require.NoError(t, inputWriter.WriteField("model", "gpt-image-2"))
			require.NoError(t, inputWriter.WriteField("prompt", "edit"))
			filePart, err := inputWriter.CreateFormFile(fieldName, "input.png")
			require.NoError(t, err)
			_, err = filePart.Write([]byte("png-data"))
			require.NoError(t, err)
			require.NoError(t, inputWriter.Close())

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(input.Bytes()))
			ctx.Request.Header.Set("Content-Type", inputWriter.FormDataContentType())

			adaptor := &Adaptor{}
			converted, err := adaptor.ConvertImageRequest(ctx, &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeImagesEdits,
			}, dto.ImageRequest{Model: "gpt-image-2", Prompt: "edit", Stream: appcommon.GetPointer(true)})
			require.NoError(t, err)

			body, ok := converted.(*bytes.Buffer)
			require.True(t, ok)
			_, params, err := mime.ParseMediaType(ctx.Request.Header.Get("Content-Type"))
			require.NoError(t, err)
			outputForm, err := multipart.NewReader(bytes.NewReader(body.Bytes()), params["boundary"]).ReadForm(1 << 20)
			require.NoError(t, err)
			defer outputForm.RemoveAll()

			require.Len(t, outputForm.File[fieldName], 1)
			require.Equal(t, "image/png", outputForm.File[fieldName][0].Header.Get("Content-Type"))
			require.Equal(t, []string{"true"}, outputForm.Value["stream"])
		})
	}
}
