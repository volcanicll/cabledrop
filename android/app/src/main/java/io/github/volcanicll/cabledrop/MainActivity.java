package io.github.volcanicll.cabledrop;

import android.app.Activity;
import android.app.DownloadManager;
import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.res.Configuration;
import android.net.Uri;
import android.os.Bundle;
import android.os.Environment;
import android.view.Gravity;
import android.view.View;
import android.view.ViewGroup;
import android.webkit.ValueCallback;
import android.webkit.WebChromeClient;
import android.webkit.WebResourceError;
import android.webkit.WebResourceRequest;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.Button;
import android.widget.EditText;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.TextView;
import android.widget.Toast;

/**
 * A thin shell around the same page the phone's browser gets at
 * http://127.0.0.1:<port>, tunnelled through the USB cable by `adb reverse`.
 *
 * Everything on that page already works in a plain WebView — the native work
 * here is only what a browser page cannot do by itself: hand real file paths
 * to the upload input, route downloads into the system DownloadManager, and
 * explain what is wrong when the cable is unplugged.
 */
public class MainActivity extends Activity {

    private static final String PREFS = "cabledrop";
    private static final String PREF_PORT = "port";
    private static final int DEFAULT_PORT = 8765;
    private static final int FILE_CHOOSER = 1001;

    private WebView web;
    private FrameLayout root;
    private LinearLayout status;
    private TextView statusTitle;
    private TextView statusBody;
    private EditText portBox;
    private SharedPreferences prefs;
    private ValueCallback<Uri[]> filePick;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        prefs = getSharedPreferences(PREFS, MODE_PRIVATE);

        // fitsSystemWindows keeps both the page and the status screen below
        // the status bar: targetSdk 35+ draws edge-to-edge by default.
        root = new FrameLayout(this);
        root.setFitsSystemWindows(true);

        web = new WebView(this);
        root.addView(web, new FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT));

        status = buildStatusView();
        status.setVisibility(View.GONE);
        root.addView(status, new FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT));

        setContentView(root, new ViewGroup.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT));

        WebSettings s = web.getSettings();
        s.setJavaScriptEnabled(true);
        s.setDomStorageEnabled(true);
        // Debug builds get a DevTools socket so the page can be inspected on a
        // real phone (`adb forward` the webview_devtools_remote socket, then
        // chrome://inspect). Release builds do not: an open debugging port on
        // a loopback page is still an open port.
        boolean debuggable =
                (getApplicationInfo().flags & android.content.pm.ApplicationInfo.FLAG_DEBUGGABLE) != 0;
        WebView.setWebContentsDebuggingEnabled(debuggable);
        // Match the page canvas so the frame between cold start and first
        // paint (and any gap under short content) is not a white flash.
        web.setBackgroundColor(resolveWindowBg());

        web.setWebViewClient(new WebViewClient() {
            @Override
            public boolean shouldOverrideUrlLoading(WebView view, WebResourceRequest request) {
                Uri url = request.getUrl();
                String host = url.getHost();
                boolean local = "127.0.0.1".equals(host) || "localhost".equals(host);
                if (local) {
                    return false; // ours: load in the WebView
                }
                // A link off the loopback has nothing to do with the bridge;
                // hand it to whatever the phone normally opens links with.
                try {
                    startActivity(new Intent(Intent.ACTION_VIEW, url));
                } catch (Exception ignored) {
                }
                return true;
            }

            @Override
            public void onReceivedError(WebView view, WebResourceRequest request,
                                        WebResourceError error) {
                if (request.isForMainFrame()) {
                    showStatus();
                }
            }
        });

        web.setWebChromeClient(new WebChromeClient() {
            // The upload input on the page is a plain <input type=file>; this
            // is what turns a tap on it into a real Android file picker.
            @Override
            public boolean onShowFileChooser(WebView view, ValueCallback<Uri[]> callback,
                                             FileChooserParams params) {
                if (filePick != null) {
                    filePick.onReceiveValue(null);
                }
                filePick = callback;
                try {
                    startActivityForResult(params.createIntent(), FILE_CHOOSER);
                    return true;
                } catch (Exception e) {
                    filePick = null;
                    return false;
                }
            }
        });

        web.setDownloadListener((url, userAgent, disposition, mime, length) -> {
            try {
                String name = filenameFrom(url, disposition);
                DownloadManager.Request req = new DownloadManager.Request(Uri.parse(url));
                req.setTitle(name);
                req.setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED);
                req.setDestinationInExternalPublicDir(Environment.DIRECTORY_DOWNLOADS, name);
                DownloadManager dm = (DownloadManager) getSystemService(Context.DOWNLOAD_SERVICE);
                dm.enqueue(req);
                Toast.makeText(this, "开始下载 " + name, Toast.LENGTH_SHORT).show();
            } catch (Exception e) {
                Toast.makeText(this, "下载失败: " + e.getMessage(), Toast.LENGTH_SHORT).show();
            }
        });

        if (savedInstanceState != null) {
            web.restoreState(savedInstanceState);
        } else {
            loadPage();
        }
    }

    private void loadPage() {
        web.loadUrl("http://127.0.0.1:" + prefs.getInt(PREF_PORT, DEFAULT_PORT) + "/");
    }

    /** Shown when the page cannot be reached: the cable is out or the desktop
     *  app is not serving. Carries the port so the rare fallback port (8765
     *  already taken on the computer) can be matched by hand. */
    private LinearLayout buildStatusView() {
        LinearLayout box = new LinearLayout(this);
        box.setOrientation(LinearLayout.VERTICAL);
        box.setGravity(Gravity.CENTER);
        box.setPadding(dp(32), dp(32), dp(32), dp(32));
        boolean night = isNightMode();
        box.setBackgroundColor(night ? 0xFF131315 : 0xFFF5F5F7);
        int titleColor = night ? 0xFFF2F2F5 : 0xFF1D1D1F;
        int bodyColor = night ? 0xFFA2A2AB : 0xFF6E6E73;

        statusTitle = new TextView(this);
        statusTitle.setText("CableDrop 未连接");
        statusTitle.setTextSize(20);
        statusTitle.setTextColor(titleColor);
        statusTitle.setGravity(Gravity.CENTER);

        statusBody = new TextView(this);
        statusBody.setText("用 USB 线连接手机和电脑，电脑端打开 CableDrop\n并保持手机网页开启，然后点下面的重试。");
        statusBody.setTextSize(14);
        statusBody.setLineSpacing(dp(3), 1f);
        statusBody.setTextColor(bodyColor);
        statusBody.setGravity(Gravity.CENTER);
        statusBody.setPadding(0, dp(10), 0, 0);

        LinearLayout row = new LinearLayout(this);
        row.setOrientation(LinearLayout.HORIZONTAL);
        row.setGravity(Gravity.CENTER);

        portBox = new EditText(this);
        portBox.setInputType(android.text.InputType.TYPE_CLASS_NUMBER);
        portBox.setHint("端口");
        portBox.setText(String.valueOf(prefs.getInt(PREF_PORT, DEFAULT_PORT)));
        LinearLayout.LayoutParams portLp = new LinearLayout.LayoutParams(dp(110), ViewGroup.LayoutParams.WRAP_CONTENT);
        row.addView(portBox, portLp);

        Button retry = new Button(this);
        retry.setText("重试");
        retry.setOnClickListener(v -> {
            int port = DEFAULT_PORT;
            try {
                port = Integer.parseInt(portBox.getText().toString().trim());
            } catch (NumberFormatException ignored) {
            }
            prefs.edit().putInt(PREF_PORT, port).apply();
            status.setVisibility(View.GONE);
            loadPage();
        });
        LinearLayout.LayoutParams retryLp = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        retryLp.setMargins(dp(10), 0, 0, 0);
        row.addView(retry, retryLp);

        box.addView(statusTitle);
        box.addView(statusBody);
        LinearLayout.LayoutParams rowLp = new LinearLayout.LayoutParams(
                ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT);
        rowLp.topMargin = dp(20);
        box.addView(row, rowLp);
        return box;
    }

    /** Whether the system is in dark mode, so the native screens can match
     *  the page's own dark palette instead of flashing white. */
    private boolean isNightMode() {
        int mask = getResources().getConfiguration().uiMode
                & Configuration.UI_MODE_NIGHT_MASK;
        return mask == Configuration.UI_MODE_NIGHT_YES;
    }

    /** Whether the page can honour prefers-color-scheme.
     *
     *  The WebView an app gets is the ROM's, and ROMs pin them for years: the
     *  phone this was developed against serves its pages from an AOSP WebView
     *  stuck at Chrome 75, where the media query does not exist and the page
     *  renders light whatever the system says. Framing a light page in a dark
     *  window looks worse than not following dark mode at all, so the frame
     *  follows the page rather than the system in that case.
     *
     *  prefers-color-scheme landed in Chrome 76. */
    private boolean pageSupportsDark() {
        if (android.os.Build.VERSION.SDK_INT < android.os.Build.VERSION_CODES.O) {
            return false;
        }
        android.content.pm.PackageInfo pkg = WebView.getCurrentWebViewPackage();
        if (pkg == null || pkg.versionName == null) {
            return false;
        }
        String major = pkg.versionName.split("\\.")[0];
        try {
            return Integer.parseInt(major) >= 76;
        } catch (NumberFormatException e) {
            return false;
        }
    }

    private int resolveWindowBg() {
        return (isNightMode() && pageSupportsDark()) ? 0xFF131315 : 0xFFF5F5F7;
    }

    /** The activity handles uiMode itself (see the manifest), so it is not
     *  recreated when the user flips dark mode: re-apply the native colours
     *  here so the frame and the offline screen keep matching the page. */
    @Override
    public void onConfigurationChanged(Configuration newConfig) {
        super.onConfigurationChanged(newConfig);
        web.setBackgroundColor(resolveWindowBg());
        rebuildStatusColors();
    }

    private void rebuildStatusColors() {
        if (status == null || statusTitle == null) return;
        boolean night = isNightMode();
        status.setBackgroundColor(night ? 0xFF131315 : 0xFFF5F5F7);
        statusTitle.setTextColor(night ? 0xFFF2F2F5 : 0xFF1D1D1F);
        statusBody.setTextColor(night ? 0xFFA2A2AB : 0xFF6E6E73);
    }

    private void showStatus() {
        status.setVisibility(View.VISIBLE);
    }

    /** The server sends `filename*=UTF-8''...` so Chinese names survive; the
     *  plain filename= form and the URL's last segment are the fallbacks. */
    private static String filenameFrom(String url, String disposition) {
        if (disposition != null) {
            java.util.regex.Matcher m = java.util.regex.Pattern
                    .compile("filename\\*=UTF-8''([^;]+)", java.util.regex.Pattern.CASE_INSENSITIVE)
                    .matcher(disposition);
            if (m.find()) {
                return Uri.decode(m.group(1));
            }
            m = java.util.regex.Pattern
                    .compile("filename=\"?([^\";]+)\"?", java.util.regex.Pattern.CASE_INSENSITIVE)
                    .matcher(disposition);
            if (m.find()) {
                return Uri.decode(m.group(1).trim());
            }
        }
        String seg = Uri.parse(url).getLastPathSegment();
        return (seg == null || seg.isEmpty()) ? "download" : Uri.decode(seg);
    }

    @Override
    protected void onActivityResult(int requestCode, int resultCode, Intent data) {
        if (requestCode == FILE_CHOOSER) {
            if (filePick != null) {
                filePick.onReceiveValue(WebChromeClient.FileChooserParams.parseResult(resultCode, data));
                filePick = null;
            }
            return;
        }
        super.onActivityResult(requestCode, resultCode, data);
    }

    @Override
    public void onBackPressed() {
        if (web.canGoBack()) {
            web.goBack();
        } else {
            super.onBackPressed();
        }
    }

    @Override
    protected void onSaveInstanceState(Bundle outState) {
        super.onSaveInstanceState(outState);
        web.saveState(outState);
    }

    private int dp(int v) {
        return Math.round(v * getResources().getDisplayMetrics().density);
    }
}
