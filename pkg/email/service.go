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

// SendExpiryReminderEmail sends a renewal reminder for active (non-cancelled) subscriptions
func (s *EmailService) SendExpiryReminderEmail(toEmail, userName, tenantName, planName, expiryDate string, daysLeft int) error {
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "https://app.warungin.com"
	}

	htmlBody := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"></head>
<body style="font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; margin: 0; padding: 0; background-color: #f5f5f5;">
    <div style="max-width: 600px; margin: 0 auto; padding: 40px 20px;">
        <div style="background: linear-gradient(135deg, #f59e0b 0%%, #d97706 100%%); border-radius: 16px 16px 0 0; padding: 32px; text-align: center;">
            <h1 style="color: white; margin: 0; font-size: 24px;">⏰ Langganan Akan Berakhir</h1>
        </div>
        <div style="background: white; padding: 32px; border-radius: 0 0 16px 16px; box-shadow: 0 4px 6px rgba(0,0,0,0.1);">
            <p style="color: #374151; font-size: 16px;">Hai <strong>%s</strong>,</p>
            <p style="color: #374151; font-size: 16px;">Langganan <strong>Warungin %s</strong> untuk <strong>%s</strong> akan berakhir dalam <strong>%d hari</strong> (<strong>%s</strong>).</p>
            <div style="background: #fffbeb; border: 1px solid #fde68a; border-radius: 12px; padding: 16px; margin: 20px 0;">
                <p style="color: #92400e; margin: 0; font-size: 14px;">Setelah berakhir, akun Anda akan otomatis beralih ke paket Gratis dengan fitur terbatas.</p>
            </div>
            <div style="text-align: center; margin: 32px 0;">
                <a href="%s/settings" style="display: inline-block; background: linear-gradient(135deg, #7c3aed 0%%, #a855f7 100%%); color: white; text-decoration: none; padding: 16px 32px; border-radius: 12px; font-weight: bold; font-size: 16px;">Perpanjang Sekarang</a>
            </div>
            <hr style="border: none; border-top: 1px solid #e5e7eb; margin: 24px 0;">
            <p style="color: #9ca3af; font-size: 12px; text-align: center;">© 2024 Warungin. All rights reserved.</p>
        </div>
    </div>
</body>
</html>
`, userName, planName, tenantName, daysLeft, expiryDate, frontendURL)

	subject := fmt.Sprintf("⏰ Langganan Warungin %s berakhir dalam %d hari", planName, daysLeft)
	return s.SendEmail(toEmail, subject, htmlBody)
}

// SendSubscriptionEndingEmail sends ending notice for cancelled subscriptions
func (s *EmailService) SendSubscriptionEndingEmail(toEmail, userName, tenantName, planName, expiryDate string, daysLeft int) error {
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "https://app.warungin.com"
	}

	htmlBody := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"></head>
<body style="font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; margin: 0; padding: 0; background-color: #f5f5f5;">
    <div style="max-width: 600px; margin: 0 auto; padding: 40px 20px;">
        <div style="background: linear-gradient(135deg, #6b7280 0%%, #4b5563 100%%); border-radius: 16px 16px 0 0; padding: 32px; text-align: center;">
            <h1 style="color: white; margin: 0; font-size: 24px;">📋 Langganan Segera Berakhir</h1>
        </div>
        <div style="background: white; padding: 32px; border-radius: 0 0 16px 16px; box-shadow: 0 4px 6px rgba(0,0,0,0.1);">
            <p style="color: #374151; font-size: 16px;">Hai <strong>%s</strong>,</p>
            <p style="color: #374151; font-size: 16px;">Sesuai permintaan pembatalan Anda, langganan <strong>Warungin %s</strong> untuk <strong>%s</strong> akan berakhir dalam <strong>%d hari</strong> (<strong>%s</strong>).</p>
            <div style="background: #f0fdf4; border: 1px solid #bbf7d0; border-radius: 12px; padding: 16px; margin: 20px 0;">
                <p style="color: #166534; margin: 0; font-size: 14px;">💡 Berubah pikiran? Anda masih bisa mengaktifkan kembali langganan sebelum tanggal berakhir.</p>
            </div>
            <div style="text-align: center; margin: 32px 0;">
                <a href="%s/settings" style="display: inline-block; background: linear-gradient(135deg, #16a34a 0%%, #22c55e 100%%); color: white; text-decoration: none; padding: 16px 32px; border-radius: 12px; font-weight: bold; font-size: 16px;">Aktifkan Kembali</a>
            </div>
            <hr style="border: none; border-top: 1px solid #e5e7eb; margin: 24px 0;">
            <p style="color: #9ca3af; font-size: 12px; text-align: center;">© 2024 Warungin. All rights reserved.</p>
        </div>
    </div>
</body>
</html>
`, userName, planName, tenantName, daysLeft, expiryDate, frontendURL)

	subject := fmt.Sprintf("📋 Langganan Warungin %s berakhir dalam %d hari", planName, daysLeft)
	return s.SendEmail(toEmail, subject, htmlBody)
}

// SendDowngradeNotificationEmail notifies when subscription is auto-downgraded to Gratis
func (s *EmailService) SendDowngradeNotificationEmail(toEmail, userName, tenantName, previousPlan string) error {
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "https://app.warungin.com"
	}

	htmlBody := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"></head>
<body style="font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; margin: 0; padding: 0; background-color: #f5f5f5;">
    <div style="max-width: 600px; margin: 0 auto; padding: 40px 20px;">
        <div style="background: linear-gradient(135deg, #ef4444 0%%, #dc2626 100%%); border-radius: 16px 16px 0 0; padding: 32px; text-align: center;">
            <h1 style="color: white; margin: 0; font-size: 24px;">ℹ️ Langganan Telah Berakhir</h1>
        </div>
        <div style="background: white; padding: 32px; border-radius: 0 0 16px 16px; box-shadow: 0 4px 6px rgba(0,0,0,0.1);">
            <p style="color: #374151; font-size: 16px;">Hai <strong>%s</strong>,</p>
            <p style="color: #374151; font-size: 16px;">Langganan <strong>Warungin %s</strong> untuk <strong>%s</strong> telah berakhir. Akun Anda sekarang menggunakan paket <strong>Gratis</strong>.</p>
            <div style="background: #fef2f2; border: 1px solid #fecaca; border-radius: 12px; padding: 16px; margin: 20px 0;">
                <p style="color: #991b1b; margin: 0 0 8px 0; font-size: 14px; font-weight: bold;">Fitur yang terbatas di paket Gratis:</p>
                <ul style="color: #991b1b; margin: 0; padding-left: 20px; font-size: 14px;">
                    <li>Maksimal 1 pengguna</li>
                    <li>Maksimal 50 produk</li>
                    <li>Maksimal 30 transaksi/hari</li>
                    <li>Retensi data 30 hari</li>
                </ul>
            </div>
            <div style="text-align: center; margin: 32px 0;">
                <a href="%s/settings" style="display: inline-block; background: linear-gradient(135deg, #7c3aed 0%%, #a855f7 100%%); color: white; text-decoration: none; padding: 16px 32px; border-radius: 12px; font-weight: bold; font-size: 16px;">Berlangganan Kembali</a>
            </div>
            <hr style="border: none; border-top: 1px solid #e5e7eb; margin: 24px 0;">
            <p style="color: #9ca3af; font-size: 12px; text-align: center;">© 2024 Warungin. All rights reserved.</p>
        </div>
    </div>
</body>
</html>
`, userName, previousPlan, tenantName, frontendURL)

	subject := "ℹ️ Langganan Warungin Anda telah berakhir"
	return s.SendEmail(toEmail, subject, htmlBody)
}

// SendCancellationConfirmationEmail confirms subscription cancellation
func (s *EmailService) SendCancellationConfirmationEmail(toEmail, userName, tenantName, planName, endDate string) error {
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "https://app.warungin.com"
	}

	htmlBody := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"></head>
<body style="font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; margin: 0; padding: 0; background-color: #f5f5f5;">
    <div style="max-width: 600px; margin: 0 auto; padding: 40px 20px;">
        <div style="background: linear-gradient(135deg, #6b7280 0%%, #4b5563 100%%); border-radius: 16px 16px 0 0; padding: 32px; text-align: center;">
            <h1 style="color: white; margin: 0; font-size: 24px;">Konfirmasi Pembatalan Langganan</h1>
        </div>
        <div style="background: white; padding: 32px; border-radius: 0 0 16px 16px; box-shadow: 0 4px 6px rgba(0,0,0,0.1);">
            <p style="color: #374151; font-size: 16px;">Hai <strong>%s</strong>,</p>
            <p style="color: #374151; font-size: 16px;">Pembatalan langganan <strong>Warungin %s</strong> untuk <strong>%s</strong> telah dikonfirmasi.</p>
            <div style="background: #f3f4f6; border-radius: 12px; padding: 20px; margin: 20px 0;">
                <p style="color: #374151; margin: 0 0 8px 0; font-size: 14px;"><strong>Tanggal berakhir:</strong> %s</p>
                <p style="color: #6b7280; margin: 0; font-size: 14px;">Anda tetap memiliki akses penuh ke semua fitur %s hingga tanggal tersebut.</p>
            </div>
            <div style="background: #f0fdf4; border: 1px solid #bbf7d0; border-radius: 12px; padding: 16px; margin: 20px 0;">
                <p style="color: #166534; margin: 0; font-size: 14px;">💡 Berubah pikiran? Anda bisa mengaktifkan kembali langganan kapan saja sebelum tanggal berakhir.</p>
            </div>
            <div style="text-align: center; margin: 32px 0;">
                <a href="%s/settings" style="display: inline-block; background: linear-gradient(135deg, #16a34a 0%%, #22c55e 100%%); color: white; text-decoration: none; padding: 16px 32px; border-radius: 12px; font-weight: bold; font-size: 16px;">Aktifkan Kembali</a>
            </div>
            <hr style="border: none; border-top: 1px solid #e5e7eb; margin: 24px 0;">
            <p style="color: #9ca3af; font-size: 12px; text-align: center;">© 2024 Warungin. All rights reserved.</p>
        </div>
    </div>
</body>
</html>
`, userName, planName, tenantName, endDate, planName, frontendURL)

	subject := fmt.Sprintf("Konfirmasi pembatalan langganan Warungin %s", planName)
	return s.SendEmail(toEmail, subject, htmlBody)
}

// SendPaymentSuccessEmail sends confirmation email after successful subscription payment
func (s *EmailService) SendPaymentSuccessEmail(toEmail, userName, tenantName, planName, billingPeriod, invoiceNumber string, amount float64, expiryDate string) error {
	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "https://app.warungin.com"
	}

	// Format billing period display
	periodDisplay := map[string]string{
		"monthly":   "Bulanan",
		"quarterly": "3 Bulan",
		"yearly":    "Tahunan",
	}[billingPeriod]

	htmlBody := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"></head>
<body style="font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; margin: 0; padding: 0; background-color: #f5f5f5;">
    <div style="max-width: 600px; margin: 0 auto; padding: 40px 20px;">
        <div style="background: linear-gradient(135deg, #16a34a 0%%, #22c55e 100%%); border-radius: 16px 16px 0 0; padding: 32px; text-align: center;">
            <h1 style="color: white; margin: 0; font-size: 28px;">✅ Pembayaran Berhasil!</h1>
        </div>
        <div style="background: white; padding: 32px; border-radius: 0 0 16px 16px; box-shadow: 0 4px 6px rgba(0,0,0,0.1);">
            <p style="color: #374151; font-size: 16px;">Hai <strong>%s</strong>,</p>
            <p style="color: #374151; font-size: 16px;">Terima kasih! Pembayaran langganan <strong>Warungin %s</strong> untuk <strong>%s</strong> telah berhasil diproses.</p>
            
            <div style="background: #f0fdf4; border: 2px solid #22c55e; border-radius: 12px; padding: 20px; margin: 24px 0;">
                <h3 style="color: #166534; margin: 0 0 16px 0; font-size: 18px;">Detail Pembayaran</h3>
                <table style="width: 100%%; border-collapse: collapse;">
                    <tr>
                        <td style="padding: 8px 0; color: #6b7280; font-size: 14px;">Nomor Invoice:</td>
                        <td style="padding: 8px 0; color: #374151; font-size: 14px; font-weight: bold; text-align: right;">%s</td>
                    </tr>
                    <tr>
                        <td style="padding: 8px 0; color: #6b7280; font-size: 14px;">Paket:</td>
                        <td style="padding: 8px 0; color: #374151; font-size: 14px; font-weight: bold; text-align: right;">%s</td>
                    </tr>
                    <tr>
                        <td style="padding: 8px 0; color: #6b7280; font-size: 14px;">Periode:</td>
                        <td style="padding: 8px 0; color: #374151; font-size: 14px; font-weight: bold; text-align: right;">%s</td>
                    </tr>
                    <tr>
                        <td style="padding: 8px 0; color: #6b7280; font-size: 14px;">Total Dibayar:</td>
                        <td style="padding: 8px 0; color: #16a34a; font-size: 18px; font-weight: bold; text-align: right;">Rp %s</td>
                    </tr>
                    <tr>
                        <td style="padding: 8px 0; color: #6b7280; font-size: 14px;">Berlaku Hingga:</td>
                        <td style="padding: 8px 0; color: #374151; font-size: 14px; font-weight: bold; text-align: right;">%s</td>
                    </tr>
                </table>
            </div>

            <div style="background: #eff6ff; border: 1px solid #bfdbfe; border-radius: 12px; padding: 16px; margin: 20px 0;">
                <p style="color: #1e40af; margin: 0; font-size: 14px;">🎉 Akun Anda sekarang memiliki akses penuh ke semua fitur %s!</p>
            </div>

            <div style="text-align: center; margin: 32px 0;">
                <a href="%s/dashboard" style="display: inline-block; background: linear-gradient(135deg, #7c3aed 0%%, #a855f7 100%%); color: white; text-decoration: none; padding: 16px 32px; border-radius: 12px; font-weight: bold; font-size: 16px;">Buka Dashboard</a>
            </div>

            <hr style="border: none; border-top: 1px solid #e5e7eb; margin: 24px 0;">
            
            <p style="color: #6b7280; font-size: 14px; margin: 16px 0;">Butuh bantuan? Hubungi kami di <a href="mailto:support@warungin.com" style="color: #7c3aed;">support@warungin.com</a></p>
            
            <p style="color: #9ca3af; font-size: 12px; text-align: center;">© 2024 Warungin. All rights reserved.</p>
        </div>
    </div>
</body>
</html>
`, userName, planName, tenantName, invoiceNumber, planName, periodDisplay, formatCurrency(amount), expiryDate, planName, frontendURL)

	subject := fmt.Sprintf("✅ Pembayaran Warungin %s Berhasil - %s", planName, invoiceNumber)
	return s.SendEmail(toEmail, subject, htmlBody)
}

// formatCurrency formats a float64 amount to Indonesian currency format
func formatCurrency(amount float64) string {
	// Simple formatting: add thousand separators
	intAmount := int(amount)
	str := fmt.Sprintf("%d", intAmount)
	
	// Add thousand separators
	n := len(str)
	if n <= 3 {
		return str
	}
	
	result := ""
	for i, digit := range str {
		if i > 0 && (n-i)%3 == 0 {
			result += "."
		}
		result += string(digit)
	}
	
	return result
}
