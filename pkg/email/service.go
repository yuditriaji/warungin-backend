package email

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

// EmailService handles sending emails via Resend API
type EmailService struct {
	apiKey    string
	fromEmail string
}

// NewEmailService creates a new email service instance
func NewEmailService() *EmailService {
	return &EmailService{
		apiKey:    os.Getenv("RESEND_API_KEY"),
		fromEmail: os.Getenv("EMAIL_FROM_ADDRESS"),
	}
}

// IsConfigured checks if the email service is properly configured
func (s *EmailService) IsConfigured() bool {
	return s.apiKey != "" && s.fromEmail != ""
}

type sendEmailRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
}

// SendEmail sends an email using Resend API
func (s *EmailService) SendEmail(to, subject, htmlBody string) error {
	if !s.IsConfigured() {
		return fmt.Errorf("email service not configured")
	}

	payload := sendEmailRequest{
		From:    s.fromEmail,
		To:      []string{to},
		Subject: subject,
		HTML:    htmlBody,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal email payload: %v", err)
	}

	req, err := http.NewRequest("POST", "https://api.resend.com/emails", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send email: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("email API returned status %d", resp.StatusCode)
	}

	return nil
}

// SendStaffInvitation sends an invitation email to a new staff member
func (s *EmailService) SendStaffInvitation(toEmail, staffName, tenantName, inviteToken, frontendURL string) error {
	inviteLink := fmt.Sprintf("%s/invite/accept?token=%s", frontendURL, inviteToken)

	htmlBody := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
</head>
<body style="font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; margin: 0; padding: 0; background-color: #f5f5f5;">
    <div style="max-width: 600px; margin: 0 auto; padding: 40px 20px;">
        <div style="background: linear-gradient(135deg, #7c3aed 0%%, #a855f7 100%%); border-radius: 16px 16px 0 0; padding: 32px; text-align: center;">
            <h1 style="color: white; margin: 0; font-size: 28px;">🎉 Selamat Datang di Warungin!</h1>
        </div>
        <div style="background: white; padding: 32px; border-radius: 0 0 16px 16px; box-shadow: 0 4px 6px rgba(0,0,0,0.1);">
            <p style="color: #374151; font-size: 16px; margin-bottom: 24px;">
                Hai <strong>%s</strong>,
            </p>
            <p style="color: #374151; font-size: 16px; margin-bottom: 24px;">
                Anda telah diundang untuk bergabung sebagai staff di <strong>%s</strong> melalui platform Warungin POS.
            </p>
            <div style="text-align: center; margin: 32px 0;">
                <a href="%s" style="display: inline-block; background: linear-gradient(135deg, #7c3aed 0%%, #a855f7 100%%); color: white; text-decoration: none; padding: 16px 32px; border-radius: 12px; font-weight: bold; font-size: 16px;">
                    Terima Undangan
                </a>
            </div>
            <p style="color: #6b7280; font-size: 14px; margin-bottom: 16px;">
                Klik tombol di atas untuk mengatur password dan mengaktifkan akun Anda.
            </p>
            <p style="color: #6b7280; font-size: 14px;">
                Link ini akan kadaluarsa dalam 7 hari.
            </p>
            <hr style="border: none; border-top: 1px solid #e5e7eb; margin: 24px 0;">
            <p style="color: #9ca3af; font-size: 12px; text-align: center;">
                Jika Anda tidak mengharapkan undangan ini, abaikan email ini.
            </p>
        </div>
        <p style="color: #9ca3af; font-size: 12px; text-align: center; margin-top: 24px;">
            © 2024 Warungin. All rights reserved.
        </p>
    </div>
</body>
</html>
`, staffName, tenantName, inviteLink)

	subject := fmt.Sprintf("Undangan Bergabung di %s - Warungin", tenantName)
	return s.SendEmail(toEmail, subject, htmlBody)
}

// SendAffiliateInvitation sends an invitation email to a new affiliator
func (s *EmailService) SendAffiliateInvitation(toEmail, affiliateName, inviteToken, portalURL string) error {
	inviteLink := fmt.Sprintf("%s/accept-invite?token=%s", portalURL, inviteToken)

	htmlBody := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
</head>
<body style="font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; margin: 0; padding: 0; background-color: #f5f5f5;">
    <div style="max-width: 600px; margin: 0 auto; padding: 40px 20px;">
        <div style="background: linear-gradient(135deg, #7c3aed 0%%, #a855f7 100%%); border-radius: 16px 16px 0 0; padding: 32px; text-align: center;">
            <h1 style="color: white; margin: 0; font-size: 28px;">🤝 Undangan Program Afiliasi</h1>
        </div>
        <div style="background: white; padding: 32px; border-radius: 0 0 16px 16px; box-shadow: 0 4px 6px rgba(0,0,0,0.1);">
            <p style="color: #374151; font-size: 16px; margin-bottom: 24px;">
                Hai <strong>%s</strong>,
            </p>
            <p style="color: #374151; font-size: 16px; margin-bottom: 24px;">
                Anda telah diundang untuk bergabung sebagai <strong>Afiliator Warungin</strong>! Sebagai afiliator, Anda akan mendapatkan komisi untuk setiap tenant yang mendaftar menggunakan kode referral Anda.
            </p>
            <div style="background: #f3f4f6; border-radius: 12px; padding: 20px; margin-bottom: 24px;">
                <h3 style="color: #374151; margin: 0 0 12px 0; font-size: 16px;">Keuntungan Menjadi Afiliator:</h3>
                <ul style="color: #6b7280; margin: 0; padding-left: 20px;">
                    <li style="margin-bottom: 8px;">Komisi 10%% dari setiap pembayaran subscription</li>
                    <li style="margin-bottom: 8px;">Dashboard untuk memantau tenant dan penghasilan</li>
                    <li style="margin-bottom: 8px;">Kode referral unik untuk dibagikan</li>
                    <li>Pembayaran komisi tepat waktu</li>
                </ul>
            </div>
            <div style="text-align: center; margin: 32px 0;">
                <a href="%s" style="display: inline-block; background: linear-gradient(135deg, #7c3aed 0%%, #a855f7 100%%); color: white; text-decoration: none; padding: 16px 32px; border-radius: 12px; font-weight: bold; font-size: 16px;">
                    Terima Undangan
                </a>
            </div>
            <p style="color: #6b7280; font-size: 14px; margin-bottom: 16px;">
                Klik tombol di atas untuk mengatur password dan mengaktifkan akun afiliator Anda.
            </p>
            <p style="color: #6b7280; font-size: 14px;">
                Link ini akan kadaluarsa dalam 7 hari.
            </p>
            <hr style="border: none; border-top: 1px solid #e5e7eb; margin: 24px 0;">
            <p style="color: #9ca3af; font-size: 12px; text-align: center;">
                Jika Anda tidak mengharapkan undangan ini, abaikan email ini.
            </p>
        </div>
        <p style="color: #9ca3af; font-size: 12px; text-align: center; margin-top: 24px;">
            © 2024 Warungin. All rights reserved.
        </p>
    </div>
</body>
</html>
`, affiliateName, inviteLink)

	subject := "Undangan Program Afiliasi Warungin"
	return s.SendEmail(toEmail, subject, htmlBody)
}
