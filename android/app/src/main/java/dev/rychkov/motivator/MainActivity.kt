package dev.rychkov.motivator

import android.app.Activity
import android.app.AlertDialog
import android.content.Context
import android.os.Bundle
import android.view.Menu
import android.view.MenuItem
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.Toast
import mobile.Mobile

class MainActivity : Activity() {

    private lateinit var web: WebView

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        startServer()
        web = WebView(this).apply {
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            webViewClient = WebViewClient()
            loadUrl(Mobile.baseURL())
        }
        setContentView(web)
    }

    override fun onDestroy() {
        super.onDestroy()
        if (isFinishing) {
            Mobile.stop()
        }
    }

    override fun onCreateOptionsMenu(menu: Menu): Boolean {
        menu.add(0, MENU_SETTINGS, 0, getString(R.string.menu_settings))
        return true
    }

    override fun onOptionsItemSelected(item: MenuItem): Boolean {
        if (item.itemId == MENU_SETTINGS) {
            showSettingsDialog()
            return true
        }
        return super.onOptionsItemSelected(item)
    }

    private fun prefs() = getSharedPreferences("bot", Context.MODE_PRIVATE)

    private fun startServer() {
        val err = Mobile.start(
            filesDir.absolutePath,
            prefs().getString("key", "") ?: "",
            prefs().getString("model", "z-ai/glm-5.3-flash") ?: "",
            prefs().getString("tasks_mcp", "") ?: "",
            prefs().getString("pomo_mcp", "") ?: "",
        )
        if (err.isNotEmpty()) {
            Toast.makeText(this, err, Toast.LENGTH_LONG).show()
        }
    }

    private fun field(hintRes: Int, key: String, def: String): EditText =
        EditText(this).apply {
            hint = getString(hintRes)
            setText(prefs().getString(key, def))
        }

    private fun showSettingsDialog() {
        val keyInput = field(R.string.key_hint, "key", "")
        val modelInput = field(R.string.model_hint, "model", "z-ai/glm-5.3-flash")
        val tasksInput = field(R.string.tasks_mcp_hint, "tasks_mcp", "")
        val pomoInput = field(R.string.pomo_mcp_hint, "pomo_mcp", "")
        val box = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(48, 24, 48, 0)
            addView(keyInput)
            addView(modelInput)
            addView(tasksInput)
            addView(pomoInput)
        }
        AlertDialog.Builder(this)
            .setTitle(getString(R.string.menu_settings))
            .setView(box)
            .setPositiveButton(getString(R.string.save)) { _, _ ->
                prefs().edit()
                    .putString("key", keyInput.text.toString().trim())
                    .putString("model", modelInput.text.toString().trim())
                    .putString("tasks_mcp", tasksInput.text.toString().trim())
                    .putString("pomo_mcp", pomoInput.text.toString().trim())
                    .apply()
                Mobile.stop()
                startServer()
                web.reload()
            }
            .setNegativeButton(getString(R.string.cancel), null)
            .show()
    }

    private companion object {
        const val MENU_SETTINGS = 1
    }
}
