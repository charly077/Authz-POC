<#import "template.ftl" as layout>
<@layout.registrationLayout; section>
    <#if section = "header">
    <#elseif section = "form">
    <form id="kc-logout-form" class="form-inline" method="post" action="${url.logoutConfirmAction}">
        <input type="hidden" name="session_code" value="${logoutConfirm.code}">
    </form>
    <script>document.getElementById('kc-logout-form').submit();</script>
    <noscript>
    <div class="split-login-wrapper">
        <div class="split-login-card">
            <div class="login-form-panel">
                <div class="brand-mark">
                    <svg width="32" height="32" viewBox="0 0 32 32" fill="none">
                        <rect width="32" height="32" rx="8" fill="#5b4cd4"/>
                        <path d="M10 16.5L14 20.5L22 12.5" stroke="white" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"/>
                    </svg>
                    <span class="brand-name">AuthZ</span>
                </div>
                <div class="welcome-text">
                    <h1>Sign<br/>Out</h1>
                    <p>${msg("logoutConfirmTitle")}</p>
                </div>
                <div class="logout-description">
                    <p>You will be signed out of your authorization workspace.</p>
                </div>
                <div class="form-actions">
                    <form class="form-inline" method="post" action="${url.logoutConfirmAction}">
                        <input type="hidden" name="session_code" value="${logoutConfirm.code}">
                        <button name="confirmLogout" id="kc-logout" type="submit" class="btn-signin btn-logout-confirm">${msg("doLogout")}</button>
                    </form>
                </div>
            </div>
        </div>
    </div>
    </noscript>
    </#if>
</@layout.registrationLayout>
