(defcustom org-sm-server-url "http://localhost:31336"
  "Host URL for SuperMemo server."
  :type 'string)

(defcustom org-sm-comment-registered-signature "org-sm-registered"
  "Signature that gets added to SM element comments to indicate they have an associated emacs entry."
  :type 'string)

(defcustom org-sm-grades '(:bad :fail :pass :good :great)
  "A sequence of grades"
  :type 'sequence)

(defun org-sm-apiclient-http-do (method url headers entity)
  "Send ENTITY and HEADERS to URL as a METHOD request.
ENTITY is a list"
  (let ((url-request-method (encode-coding-string method 'us-ascii))
        (url-request-extra-headers '())
        (url-request-data (encode-coding-string (json-encode-list entity) 'utf-8))
        (url-mime-charset-string (url-mime-charset-string))
        (url-mime-language-string nil)
        (url-mime-encoding-string nil)
        (url-mime-accept-string nil)
        (url-personal-mail-address nil))

    (dolist (header headers)
      (let* ((mapped (assoc-string (downcase (car header))
                                   '(("from" . url-personal-mail-address)
                                     ("accept-encoding" . url-mime-encoding-string)
                                     ("accept-charset" . url-mime-charset-string)
                                     ("accept-language" . url-mime-language-string)
                                     ("accept" . url-mime-accept-string)))))

        (if mapped
            (set (cdr mapped) (encode-coding-string (cdr header) 'us-ascii))
          (let* ((hkey (encode-coding-string (car header) 'us-ascii))
                 (hvalue (encode-coding-string (cdr header) 'us-ascii)))
            (setq url-request-extra-headers (cons (cons hkey hvalue) url-request-extra-headers))))))

    (setq org-sm-apiclient-within-call t)
    (setq org-sm-apiclient-request-time-start (current-time))
    (url-retrieve-synchronously url nil nil 10)))

(defun org-sm-apiclient-http-with-sm-server-url-do (method path &optional entity headers)
  "Send HEADERS and ENTITY to PATH of SuperMemo server host url as a METHOD request.
ENTITY is a list, is default empty. Headers is default '((\"Content-Type\" . \"application/json\"))"
  (org-sm-apiclient-http-do method
                             (concat org-sm-server-url "/" path)
                             (if headers headers '(("Content-Type" . "application/json")))
                             entity))

(defun org-sm-apiclient-http-with-sm-server-url-do-and-parse-current (method path &optional entity headers)
  "Send HEADERS and ENTITY to PATH of SuperMemo server host url as a METHOD request.
ENTITY is a list, is default empty. Headers is default '((\"Content-Type\" . \"application/json\"))"
  (with-current-buffer
      (org-sm-apiclient-http-with-sm-server-url-do method path entity headers)
    (save-excursion
      (goto-char (point-min))
      (search-forward "{")
      (backward-char)
      (json-parse-buffer))))

(defun org-sm-apiclient-http-ping ()
   (org-sm-apiclient-http-response-p
    (org-sm-apiclient-http-with-sm-server-url-do-and-parse-current "GET" "ping")))

(defun org-sm-apiclient-element-back ()
   (org-sm-apiclient-http-response-p
    (org-sm-apiclient-http-with-sm-server-url-do-and-parse-current "GET" "element-back")))

(defun org-sm-apiclient-element-forward ()
   (org-sm-apiclient-http-response-p
    (org-sm-apiclient-http-with-sm-server-url-do-and-parse-current "GET" "element-forward")))

(defun org-sm-apiclient-http-response-result-eq (response result)
  (equal (gethash "result" response "false") result))

(defun org-sm-apiclient-http-response-p (response)
  (org-sm-apiclient-http-response-result-eq response "true"))

(defun org-sm-apiclient-graded-p ()
   (org-sm-apiclient-http-response-p
    (org-sm-apiclient-http-with-sm-server-url-do-and-parse-current "GET" "was-graded")))

(defun org-sm-apiclient-set-grade (grade)
  (org-sm-apiclient-http-response-p
   (org-sm-apiclient-http-with-sm-server-url-do-and-parse-current
    "POST"
    "set-grade"
    (list (cons "grade" grade)))))

(defun org-sm-apiclient-set-grade-s (grade)
  ;TODO maybe i'm deprecating this...
  (org-sm-apiclient-http-response-p
   (let ((gradepos (cl-position grade org-sm-grades)))
     (org-sm-apiclient-http-with-sm-server-url-do-and-parse-current
      "POST"
      "set-grade"
      (list (cons "grade" gradepos))))))

(defun org-sm-apiclient-next-repetition ()
  "Used to go to next repetition, only to be used to move forward from TODO nocheckin- finish doc"
  (org-sm-apiclient-http-response-p
   (org-sm-apiclient-http-with-sm-server-url-do-and-parse-current "GET" "next-repetition")))

(defun org-sm-apiclient-element-info ()
  (org-sm-apiclient-http-with-sm-server-url-do-and-parse-current "GET" "element-info"))

(defun org-sm-apiclient-postpone (days)
  "Reschedule next interval for element DAYS number of days in the future."
  (when (and (integerp days) (< 0 days))
    (org-sm-apiclient-http-response-p
     (org-sm-apiclient-http-with-sm-server-url-do-and-parse-current "POST" "postpone"  (list (cons "days" days))))))

(defun org-sm-apiclient-set-priority-a-factor (id priority afactor)
  (org-sm-apiclient-http-response-p
   (org-sm-apiclient-http-with-sm-server-url-do-and-parse-current
    "POST"
    "set-priority-a-factor"
    (list (cons "uuid" id)
          (cons "priority" priority)
          (cons "afactor" afactor)))))

(defun org-sm-apiclient-element-create (id priority type &optional afactor)
  (org-sm-apiclient-http-response-p
   (cl-case type
      (:item
       (org-sm-apiclient-http-with-sm-server-url-do-and-parse-current "POST" "new-item" (list
                                                                                         (cons "uuid" id)
                                                                                         (cons "priority" priority))))
      (:topic
       (org-sm-apiclient-http-with-sm-server-url-do-and-parse-current "POST" "new-topic" (list
                                                                                          (cons "uuid" id)
                                                                                          (cons "priority" priority)
                                                                                          (cons "afactor" (or afactor 1.2))))))))

(defun org-sm-apiclient-get-element-id ()
  (let ((response (org-sm-apiclient-http-with-sm-server-url-do-and-parse-current "GET" "current-element-id")))
    (let* ((result (gethash "result" response "")))
      (when (string-match-p (regexp-quote org-sm-comment-registered-signature) result)
        (string-match "<\\(.*\\)>" result)
        (substring (match-string 0 result) 1 -1)))))

(defun org-sm-apiclient-get-element-priority ()
  "Gets priority of current element before any changes made recently (This is a limitation of the api server)."
  (gethash "Priority" (org-sm-apiclient-element-info)))

(defun org-sm-apiclient-item-p ()
  (let ((type (gethash "ElementType" (org-sm-apiclient-element-info))))
    (equal "Item" type)))

(defun org-sm-apiclient-dismissed-p (&optional element-info)
  (let ((status (gethash "ElementStatus" (or element-info (org-sm-apiclient-element-info)))))
    (equal "Dismissed" status)))

(defun org-sm-apiclient-dismiss (id)
   (org-sm-apiclient-http-response-p
    (org-sm-apiclient-http-with-sm-server-url-do-and-parse-current "POST" "dismiss" (list (cons "uuid" id)))))

(defun org-sm-apiclient-set-element-content (content)
  (org-sm-apiclient-http-response-p
    (org-sm-apiclient-http-with-sm-server-url-do-and-parse-current
     "POST"
     "set-element-content"
     (list (cons "text" content)))))

(defun org-sm-apiclient-set-element-title (title)
  (org-sm-apiclient-http-response-p
    (org-sm-apiclient-http-with-sm-server-url-do-and-parse-current
     "POST"
     "set-element-title"
     (list (cons "text" title)))))

;(require 'org-ov-highlighter)
(require 'org-web-tools)
(require 'org-roam-node)

(defvar org-sm-node-current-id nil
  "Contains org-element for current node in learning process.")

(defun org-sm-node-element-type-read-s (type-s)
  "Prompts user to enter element type. Returns element type as string."
  (interactive
   (list
    (completing-read "Choose Element Type: " '(":topic" ":item"))))
  type-s)

(defun org-sm-node-grade-read ()
  "Prompts user to enter grade"
  (let ((input nil))
    (while (not (memq (setq input (read-key "Enter Grade (1-4): "))
                      (list ?1 ?2 ?3 ?4 7))))
    (unless (eq input 7)
      (- input ?1))))

(defun org-sm-node-postpone-days-read (initial-days)
  "Prompts user to enter days"
  (let ((days (read-number "Enter New Interval (days): " initial-days)))
    (when (and (<= 1 days) (integerp days))
    days)))

(defun org-sm-node-type-read (initial-type)
  (interactive (list (completing-read "Type: " '(":topic" ":item"))))
  initial-type)

(defun org-sm-node-priority-read (initial-priority)
  "Prompts user to enter priority"
  (let ((priority (read-number "Enter Priority: " initial-priority)))
    (unless (or (>  priority 100)
                (<= priority 0))
    priority)))

(defun org-sm-node-set-type-tags-at-point (type)
  (save-excursion
    (org-back-to-heading)
    (let ((tags (org-get-tags)))
      (add-to-list 'tags (substring (symbol-name type) 1))
      (add-to-list 'tags "drill")
      (org-set-tags tags)
      (message "Type tags SET at point"))))

(defun org-sm-node-export-at-point (&optional extract-parent-id extract-parent-point)
  "Exports node at point to supermemo as element. If EXTRACT-PARENT-ID is non-nil, it creates an extract."
  (let ((content (buffer-substring-no-properties
                  (org-element-property :contents-begin (org-element-at-point))
                  (org-element-property :contents-end (org-element-at-point))))
        ;(title (org-element-property :title (org-element-at-point)))
        (id (org-sm-id-at-point-or-create))
        (priority-s (or (org-entry-get (point) "SM_PRIORITY") "66.0"))
        (type (intern (org-entry-get (point) "SM_ELEMENT_TYPE"))))
    (message "id: %s" id)
    (setq org-sm-node-current-id id)
    (org-sm-node-set-type-tags-at-point type)
    (when extract-parent-id
      (org-entry-put (point) "SM_EXTRACT_PARENT_ID" extract-parent-id))
    (when extract-parent-point
      (org-entry-put (point) "SM_EXTRACT_PARENT_POINT" extract-parent-point))
    (message "Before calling org-sm-apiclient-element-create")
    (org-sm-apiclient-element-create id (float (string-to-number priority-s)) type)))

;; TODO nocheckin this is never called -- delete
;;(defun org-sm-node-convert-and-export-at-point (&optional extract-parent-id priority type-s force-new-id)
;;  "Converts org entry at point to SM node and exports to supermemo as element"
;;  ; TODO verify that you're at a valid org entry at this point
;;  (let ((priority (or priority (org-sm-node-priority-read 33.3)))
;;        (type-s (or type-s (call-interactively 'org-sm-node-element-type-read-s))))
;;      (org-back-to-heading t)
;;      (org-entry-put (point) "SM_PRIORITY" (number-to-string priority))
;;      (org-entry-put (point) "SM_ELEMENT_TYPE" type-s)
;;      (when force-new-id (org-id-get-create))
;;      (org-sm-node-export-at-point extract-parent-id)))

(defun org-sm-capture-node-prepare-finalize-maybe-abort ()
  (when (and org-note-abort
             (org-capture-get :element-type))
    (org-sm-apiclient-dismiss (org-roam-id-at-point))))

(defun org-sm-node-current-element-present-as-hidden-non-answer-text (id)
  (message "hiding item cloze")
  (org-sm-unhide-text)
  (widen)
  (org-sm-id-goto id)
  (org-with-wide-buffer
   (let* ((org-entry-beg (org-element-property :begin (org-element-at-point)))
          (cloze-beg (string-match (regexp-quote "[[cloze:") (buffer-string) org-entry-beg))
          (cloze-end (+ 1 (string-match (regexp-quote "]") (buffer-string) cloze-beg)))
          (cloze-description-end (string-match (regexp-quote "]]") (buffer-string) cloze-end)))
     (org-sm-hide-region (+ 1 cloze-beg) (+ 9 cloze-beg))
    (org-sm-hide-region cloze-end (+ 3 cloze-description-end)))))

(defun org-sm-node-current-element-present-as-hidden-cloze-text (id)
(message "hiding item cloze")
(widen)
(org-sm-unhide-text)
(org-sm-id-goto id)
(org-with-wide-buffer
;; Hide the cloze regions
(let* ((org-entry-beg (org-element-property :begin (org-element-at-point)))
        (org-entry-end (org-element-property :end (org-element-at-point)))
        (cloze-beg (string-match (regexp-quote "[[cloze:") (buffer-string) org-entry-beg))
        (cloze-end (+ 3 (string-match (regexp-quote "]") (buffer-string) cloze-beg)))
        (cloze-description-end (string-match (regexp-quote "]]") (buffer-string) cloze-end)))
    (org-sm-hide-region (+ 1 cloze-beg) cloze-end)
    (org-sm-hide-region (+ 1 cloze-description-end) (+ 3 cloze-description-end))
    ;; Hide headings with :hide: tag in the same active area
    (save-excursion
    (while (re-search-forward org-outline-regexp-bol org-entry-end t)
        (when (member "hide" (org-get-tags))
        (org-fold-hide-subtree)))))))

(defun org-sm-capture-node-after-finalize-maybe-hide-cloze-text ()
                                        ;TODO finish the docstring which describes what this is doing because it's confusing as fuck
  "This fun is waiting for the immediate-finish org-capture for importing items to finish so that it can zip to the item in the file buffer and set the overlays for the item."
  (when-let ((_ (and (org-capture-get :sm-import-item)
                     (org-capture-get :immediate-finish)))
             (id (org-capture-get :sm-import-id)))
    (message "After sm-import capture immediate finish item finalization. Id is: %s" id)
    (org-sm-node-current-element-present-as-hidden-cloze-text id)))

(defun org-sm-capture-node-before-finalize-maybe-back-to-original-element ()
  (when-let ((original-current-id (org-capture-get :sm-extract-original-current-id)))
    (setq org-sm-node-current-id original-current-id)))

(defun org-sm-node-export-at-point-interactive ()
  (interactive)
  (org-back-to-heading)
  (let* ((context (org-element-at-point))  ;; Get the element at point
         (tags (org-element-property :tags context)))  ;; Extract the tags
    (if (and tags (member "drill" tags))  ;; Check if :drill: tag is present
        (when-let ((priority-s (or (org-entry-get (point) "SM_PRIORITY") "66.0"))
                   (afactor-s (or (org-entry-get (point) "SM_A_FACTOR") "1.2"))
                   (priority (string-to-number priority-s))
                   (afactor (string-to-number afactor-s))
                   (id (org-roam-id-at-point)))
          (message ":drill tag found at point.")
          (org-sm-apiclient-set-priority-a-factor id priority afactor))
      (message "No :drill tag at point. %s" tags)
      (let ((priority-s (number-to-string (org-sm-node-priority-read 33)))
            (type-s (call-interactively 'org-sm-node-type-read)))
        (org-entry-put (point) "SM_PRIORITY" priority-s)
        (org-entry-put (point) "SM_ELEMENT_TYPE" type-s)
        (org-sm-node-export-at-point)))))


(defun org-sm-unhide-text-interactive ()
  "This function is helpful for modifying the cloze. It reveals the full cloze markup syntax (which is normally hidden) so you can modify it."
  (interactive)
  (org-sm-unhide-text))

(defun org-sm-capture-node-maybe-create ()
  (when-let ((type (plist-get org-capture-plist :element-type)))
    (message "org-sm-capture-node-maybe-create has been called %s" org-capture-plist)
    (let* ((original-link (plist-get org-capture-plist :annotation))
           (original-description
            (or (plist-get org-capture-plist :sm-extract-new-title)
                (replace-regexp-in-string "\\(\\[\\[.*\\]\\[\\)\\(.*?\\)\\]\\]" "\\2" original-link)))
           (_ (insert original-description))
           (parent-id (plist-get org-capture-plist :sm-extract-parent-id))
           (parent-point (plist-get org-capture-plist :sm-extract-parent-point))
           (create-under-subtree (and parent-id (plist-get org-capture-plist :sm-extract-create-under-subtree)))
           (new-id (plist-get org-capture-plist :sm-extract-new-id))
           (priority-s (plist-get org-capture-plist :priority))
           (original-content (plist-get org-capture-plist :sm-original-content))
           (_ (message "priority is: %s" priority-s))
           (_ (when (plist-get org-capture-plist :ask-priority)
                (setq priority-s (number-to-string (org-sm-node-priority-read (string-to-number (or priority-s "33"))))))))
      (message "extract-parent-id: %s" parent-id)
      (org-entry-put (point) "SM_PRIORITY" priority-s)
      (org-entry-put (point) "SM_ELEMENT_TYPE" (symbol-name type))
      (when new-id
        (message "setting new id for captured node: %s" new-id)
        (org-entry-put (point) "ID" new-id))
      (when original-content
        (org-with-point-at (org-element-property :contents-end (org-element-at-point))
          (insert original-content "\n")))
      (org-sm-node-export-at-point parent-id parent-point)
      (save-excursion
        (when create-under-subtree
          (org-cut-subtree)))
      (org-sm-capture-do-to-original-buffer
       '(progn
          (deactivate-mark)
          (when create-under-subtree
            (org-paste-subtree (1+ (org-current-level)))))))))

; nocheckin
; TODO I can simplify this now because there's no longer any importing from Supermemo needed
;      And these notes may be outdated:
    ; TODO I can modify this so that it takes the previous UUID, and replaces it....
    ; TODO I kinda want to remove this feature...
    ; TODO I would rather delete from the MWDB if this doesn't exist in emacs
    ; TODO so then I probably want to delete this.
(defun org-sm-capture-node-maybe-smimport ()
  (when-let* ((element-info (org-capture-get :sm-import-element-info))
              (type (cdr (assoc-string
                          (downcase (gethash "ElementType" element-info "topic"))
                          '(("topic" . :topic)
                            ("item" . :item)))))
              (priority (gethash "Priority" element-info ""))
              (contents (org-web-tools--html-to-org-with-pandoc
                         (let ((content (gethash "Content" element-info "")))
                           (message "Sm-importing elem with content: %s" content)
                           (if-let* ((_ (eq type :item))
                                     (answer (gethash "Answer" element-info))
                                     (display-text-beg (string-match
                                                        "<SPAN class=cloze>"
                                                        content))
                                     (display-text-end (string-match
                                                        "</SPAN>"
                                                        content
                                                        display-text-beg))
                                     (left-string (substring content 0 display-text-beg))
                                     (right-string (substring content (+ 7 display-text-end)))
                                     (_ (message "before middle string"))
                                     (middle-string (replace-regexp-in-string (regexp-quote "[") "{" (substring content (+ 18 display-text-beg) display-text-end)))
                                     (_ (message "after first middle string"))
                                     (middle-string (replace-regexp-in-string (regexp-quote "]") "}" middle-string)))
                               (concat left-string "[[cloze:" answer "][" middle-string "]]" right-string)
                             content))))
              (title (gethash "Title" element-info "Untitled Element Imported From SM")))
    ; TODO make sure if it's an item and you're ready-to-grade, it doesn't reveal the answer
    (save-excursion
      (message "In smimport capture and setting title: %s" title)
      (org-edit-headline title))
    (save-excursion
      (insert "\n" contents))
    (org-sm-node-set-type-tags-at-point type)
    (if-let ((id (org-capture-get :sm-import-id))
             (_ (message "Importing from sm and id is: %s" id)))
        (and (org-entry-put (point) "ID" id)
             (setq org-sm-node-current-id id)))
    (org-entry-put (point) "SM_PRIORITY" priority)
    (org-entry-put (point) "SM_ELEMENT_TYPE" (symbol-name type))))

;TODO I should only add these hooks in a lexical closure of these functions where they're used
(add-hook 'org-capture-mode-hook #'org-sm-capture-node-maybe-create)
(add-hook 'org-capture-mode-hook #'org-sm-capture-node-maybe-smimport)
(add-hook 'org-capture-prepare-finalize-hook #'org-sm-capture-node-prepare-finalize-maybe-abort)
(add-hook 'org-capture-after-finalize-hook #'org-sm-capture-node-after-finalize-maybe-hide-cloze-text)
(add-hook 'org-capture-before-finalize-hook #'org-sm-capture-node-before-finalize-maybe-back-to-original-element)

; Add the extract templates
;;(add-to-list 'org-capture-templates
;;      '("z" "cloze" entry (file "~/org/extracts.org")
;;        "* %? (cloze) \n%U\n\n" :clock-in t :clock-resume t :element-type :item))
;;(add-to-list 'org-capture-templates
;;      '("x" "extract" entry (file "~/org/extracts.org")
;;        "* %? (extract) \n%U\n\n%i\n" :clock-in t :clock-resume t :element-type :topic))
;; TODO nocheckin remove ?
(add-to-list 'org-capture-templates
      '("z" "cloze" entry (file "~/org/extracts.org")
        "* %? (cloze) \n%U\n%a\n\n" :clock-in t :clock-resume t :element-type :item))
(add-to-list 'org-capture-templates
      '("x" "extract" entry (file "~/org/extracts.org")
        "* %? (extract) \n%U\n%a\n\n%i\n" :clock-in t :clock-resume t :element-type :topic))

;; Add the extract templates
(add-to-list 'org-capture-templates
      '("s" "sm-import" entry (file "~/org/extracts.org")
        "* \n%U\n\n%?\n" :clock-in t :clock-resume t))

;TODO Make sure the extracts use org-id in their :annotations
;TODO ^ for this read this: https://www.gnu.org/software/emacs/manual/html_node/org/Handling-Links.html
;TODO To fix this, I might need to do something to org-store-link
;TODO It's really important that these links *just work*

(defun org-sm-capture-do-to-original-buffer (fn)
  "Capture the active region of the pdf-view buffer."
  (let* ((orig-buf-name (plist-get org-capture-plist :original-buffer))
         (orig-buf (get-buffer orig-buf-name)))
    (if (buffer-live-p orig-buf)
        (with-current-buffer orig-buf (eval fn))
      (user-error "Buffer %S not alive." orig-buf-name))))

(defun org-sm-node-extract ()
  "TODO Docstring. Use 1 prefix argument for putting the extract in the subtree. Use 2 prefix arguments to specify the priority. Use 3 prefix arguments for no-immediate-finish."
  (interactive)
  (widen)
  (unwind-protect
    (when (region-active-p)
      ;; Group functions together to avoid inconsistent state on quit
      (atomic-change-group
        (org-sm-apiclient-http-ping)
        ;(org-ov-highlight-blue)
        (let* ((org-id-link-to-org-use-id t)
               (current-prefix-arg- (when current-prefix-arg (car current-prefix-arg)))
               (immediate-finish (not (eq current-prefix-arg- 32)))
               (create-under-subtree (eq current-prefix-arg- 4))
               ;; nocheckin :: The problem with calling org-sm-id-at-point-or-create.. It doesn't need to do this
               (parent-id (let ((org-sm-node-current-id (org-sm-id-at-point)))
                            (call-interactively 'org-sm-read-point-set)
                            org-sm-node-current-id))
               (parent-point (number-to-string (org-sm-current-point-relative)))
               ;(_ (message "parent id: %s. original-id: %s" parent-id org-sm-node-current-id))
               (priority (or (org-entry-get (org-id-find parent-id t) "SM_PRIORITY") "33"))
               (_ (message "priority is %s" priority))
               (_ (message "create-under-subtree is %s. current-prefix-arg is %s" create-under-subtree current-prefix-arg))
               (org-capture-templates
                (if immediate-finish
                    (mapcar (lambda (template)
                              (append template
                                      (list :immediate-finish t)
                                      (list :sm-extract-create-under-subtree create-under-subtree)
                                      (list :sm-extract-original-current-id org-sm-node-current-id)
                                      (list :sm-extract-parent-id parent-id)
                                      (list :sm-extract-parent-point parent-point)
                                      (list :priority priority)
                                      (when (eq current-prefix-arg- 16) (list :ask-priority t))))
                            org-capture-templates)
                  org-capture-templates))
               )
          (org-capture nil "x"))))
    (deactivate-mark)))

(defun org-sm-node-generate-cloze ()
  "TODO Docstring. Use 1 prefix argument for putting the extract in the subtree. Use 2 prefix arguments to specify the priority. Use 3 prefix arguments for no-immediate-finish."
  (interactive)
  (widen)
  (unwind-protect
    (when (region-active-p)
      ;; Group functions together to avoid inconsistent state on quit
      (atomic-change-group
        (org-sm-apiclient-http-ping)
        ;(org-ov-highlight-blue)
        (let* ((org-id-link-to-org-use-id t)
               (current-prefix-arg- (when current-prefix-arg (car current-prefix-arg)))
               (immediate-finish (not (eq current-prefix-arg- 32)))
               (create-under-subtree (eq current-prefix-arg- 4))
               (parent-id (let ((org-sm-node-current-id (org-sm-id-at-point)))
                            (call-interactively 'org-sm-read-point-set)
                            org-sm-node-current-id))
               ;(_ (message "parent id: %s. original-id: %s" parent-id org-sm-node-current-id))
               (priority (or (org-entry-get (org-id-find parent-id t) "SM_PRIORITY") "33"))
               (cloze-begin (min (point) (mark)))
               (cloze-end (max (point) (mark)))
               (contents-begin (progn
                                 (org-back-to-heading t)
                                 (min cloze-begin (point-at-bol 2))))
               (contents-end (max cloze-end (org-end-of-subtree t)))
               (content (replace-regexp-in-string ":PROPERTIES:\\(.\\|\n\\)*:END:" "\n"
                         (concat
                          (buffer-substring-no-properties contents-begin cloze-begin)
                          "[[cloze:"
                          (buffer-substring-no-properties cloze-begin cloze-end)
                          "][{...}]]"
                          (buffer-substring-no-properties cloze-end contents-end))))
               (_ (message "priority is %s" priority))
               (org-capture-templates
                (if immediate-finish
                    (mapcar (lambda (template)
                              (append template
                                      (list :immediate-finish t)
                                      (list :sm-original-content content)
                                      (list :sm-extract-create-under-subtree create-under-subtree)
                                      (list :sm-extract-original-current-id org-sm-node-current-id)
                                      (list :sm-extract-parent-id parent-id)
                                      (list :priority priority)
                                      (when current-prefix-arg (list :ask-priority t))))
                            org-capture-templates)
                  org-capture-templates)))
          (org-capture nil "z"))))
    (deactivate-mark)))

(defun org-sm-id-at-point-or-create ()
  (let ((org-roam-id (org-roam-id-at-point)))
    (if (or (not org-roam-id) (eq (org-find-entry-with-id org-roam-id) 1))
        (org-id-get-create)
      org-roam-id)))

(defun org-sm-id-goto (id)
  ;TODO look into ignore-errors
  (interactive "sID: ")
  (when-let ((m (org-id-find id 'marker)))
    (org-pop-to-buffer-same-window (marker-buffer m))
    (goto-char m)
    (move-marker m nil)
    (org-show-context)
    t))

;; TODO nocheckin remove
;;(defun org-sm-node-set-priority-at-point ()
;;  (interactive)
;;  (let* ((id (org-roam-id-at-point))
;;         (current-priority (string-to-number (org-entry-get (point) "SM_PRIORITY")))
;;         (priority (org-sm-node-priority-read current-priority)))
;;    (org-entry-put (point) "SM_PRIORITY" (number-to-string priority))
;;    (org-sm-apiclient-set-priority id (float priority))))

(defun org-sm-goto-current ()
  "Go to current element in emacs. Send org-sm-goto-next if current element is graded item."
  (interactive)
  (call-interactively 'org-sm-node-goto-element-id-or-smimport)
  (when (and (org-sm-apiclient-graded-p) ; This goes first, because it's faster.
             (org-sm-apiclient-item-p))
    (org-sm-goto-next))
  (setq org-sm-node-current-id (org-roam-id-at-point))) ; TODO Do I need to rely on org-roam?

(defun org-sm-node-maybe-dismiss-at-point ()
  (interactive)
  (let ((mystate (or (and (fboundp 'org-state)
                          state)
                     (nth 2 (org-heading-components)))))
    (when (and (org-id-get) ; confirm it has an id
               (member "drill" (org-get-tags))
               (message "Found drill in tags")
               (equal mystate "DONE")
               (message "Found DONE in todo state"))
      (org-sm-node-dismiss-at-point))))

(defun org-sm-node-dismiss-at-point ()
  (interactive)
  (org-sm-apiclient-dismiss (org-roam-id-at-point))
  (let ((tags (org-get-tags)))
    (add-to-list 'tags "dismissed")
    (org-set-tags tags)))

(defun org-sm-node-postpone ()
  (interactive)
  (when-let ((days (org-sm-node-postpone-days-read 1)))
    (org-sm-apiclient-postpone days)))

(defun org-sm-goto-next ()
  (interactive)
  (let ((itemp (org-sm-apiclient-item-p)))
    (if (and itemp (not (org-sm-apiclient-graded-p))) ; This goes first, because it's faster.
        (org-sm-node-answer)
      (unless itemp (org-sm-update-interval-properties))
      ;; Otherwise, pop the learning queue and move to the next repetition.
      (org-sm-apiclient-next-repetition)
      (call-interactively 'org-sm-node-goto-element-id-or-smimport)
      (evil--jumps-push)
      (setq org-sm-node-current-id (org-roam-id-at-point))))) ; TODO Do I need to rely on org-roam?

(defun org-sm-update-interval-properties ()
  "Update SM_LAST_REVIEW and SM_INTERVAL properties for the current org entry."
  (interactive)
  ;; Ensure the ID property is available
  (let ((entry-id org-sm-node-current-id))
    (when entry-id
      (save-excursion
        ;; Find the entry with the specified ID
        (org-id-goto entry-id)
        (let ((sm-a-factor (or (org-entry-get (point) "SM_A_FACTOR") "1.2"))
              (sm-interval (or (org-entry-get (point) "SM_INTERVAL") "1.0"))
              (today (format-time-string "[%Y-%m-%d %a %H:%M]")))
          (when (and sm-a-factor sm-interval)
            ;; Update SM_LAST_REVIEW with today's date
            (org-entry-put (point) "SM_LAST_REVIEW" today)
            ;; Calculate new interval
            (let* ((a-factor (string-to-number sm-a-factor))
                   (interval (string-to-number sm-interval))
                   (new-interval (* interval a-factor)))
              ;; Update SM_INTERVAL
              (org-entry-put (point) "SM_INTERVAL" (number-to-string new-interval)))))))))


(defun org-sm-id-at-point ()
  "Find the nearest element (including the current one) with the tag 'drill' and return its ID."
  (save-excursion
    (let ((nearest-ancestor nil))
      ;; Check the current element first
      (when (and (member "drill" (org-get-tags nil t))  ; Check for 'drill' tag in current element
                 (org-id-get))                   ; Check for ID in current element
        (setq nearest-ancestor (org-id-get)))
      ;; Traverse ancestors if no match was found
      (while (and (not nearest-ancestor) (org-up-heading-safe))
        (when (and (member "drill" (org-get-tags nil t))  ; Check for 'drill' tag in ancestor
                  (org-id-get))                   ; Check for ID in ancestor
          (setq nearest-ancestor (org-id-get))))
      ;; Display the result
      (if nearest-ancestor
          (message "SUCCESS: Nearest element with tag 'drill' has ID: %s" nearest-ancestor)
        (message "No element or ancestor with tag 'drill' found"))
      nearest-ancestor)))


(defun org-sm-read-point-goto ()
  "Goto the relative point stored in the 'SM_POINT' property of the element with the 'drill' tag and ID."
  (interactive)
  (when-let ((id (org-sm-id-at-point)))
    (let ((entry-pos (org-entry-get (org-id-find id t) "SM_POINT")))
      (if entry-pos
          (let ((heading-pos (org-get-heading-position id)))
            (goto-char (+ heading-pos (string-to-number entry-pos))))
        (message "No point saved for this element with ID: %s." id)))))

(defun org-sm-read-point-set ()
  "Set the relative point in the 'SM_POINT' property of the element with the 'drill' tag and ID."
  (interactive)
  (when-let* ((id (org-sm-id-at-point))
              (pos (point)))
    (let ((entry (org-id-find id t)))
      (when entry
        (let ((heading-pos (org-get-heading-position id))) ;; Get the position of the heading
          (org-entry-put entry "SM_POINT" (number-to-string (- pos heading-pos))) ;; Save the relative point
          (message "Saved relative point %d (relative to heading) in 'SM_POINT' property for entry with ID: %s" (- pos heading-pos) id))))))

(defun org-sm-current-point-relative ()
  "Return the current point relative to the heading with the 'drill' tag and ID."
  (when-let ((id (org-sm-id-at-point)))
    (let ((heading-pos (org-get-heading-position id))) ;; Get the heading position
      (- (point) heading-pos)))) ;; Return the relative position

(defun org-get-heading-position (id)
  "Return the position of the heading with the given ID."
  (save-excursion
    (org-id-goto id)
    (line-beginning-position)))


(setplist 'org-sm-hidden-text-overlay
          '(invisible t))

(defun org-sm-node-answer (&optional grade)
  "If current element has id, go to node with id. If current element has no Id, import element using org-capture."
  (interactive)
  (org-sm-id-goto org-sm-node-current-id)
  (when (org-sm-apiclient-item-p)
    (message "answering")
    (org-sm-unhide-text)
    (org-sm-node-current-element-present-as-hidden-non-answer-text org-sm-node-current-id)
    (let* (successfully-graded)
      (if-let* ((grade (or grade (org-sm-node-grade-read))))
          (and (org-sm-apiclient-set-grade grade)
               (message "Grade sent: %s" (+ 1 grade))
               (setq successfully-graded t))
        (unless successfully-graded
          (org-sm-unhide-text)
          (org-sm-node-current-element-present-as-hidden-cloze-text org-sm-node-current-id))))))

(defun org-sm-unhide-text ()
  "Unhide text. (Same as org-drill-unhide-text)"
  ;TODO make it also clear the displayed answer properly
  (save-excursion
    (org-with-wide-buffer
     (dolist (ovl (overlays-in (point-min) (point-max)))
       (when (eql 'org-sm-hidden-text-overlay (overlay-get ovl 'category))
         (delete-overlay ovl))))))

(defun org-sm-hide-region (beg end &optional text)
  "Same as org-drill-hide-region."
  (let ((ovl (make-overlay beg end)))
    (overlay-put ovl 'category
                 'org-sm-hidden-text-overlay)
    (overlay-put ovl 'priority 9999)
    (when (stringp text)
      (overlay-put ovl 'invisible nil)
      (overlay-put ovl 'face 'default)
      (overlay-put ovl 'display text))))

(defun org-sm-node-goto-element-id-or-smimport ()
  "If current element has id, go to node with id. If current element has no Id, import element using org-capture."
  (interactive)
  (widen)
  (let* ((itemp (org-sm-apiclient-item-p))
         (sm-element-id (org-sm-apiclient-get-element-id))
         (should-import (or (not sm-element-id) (not (org-sm-id-goto sm-element-id)))))
    (message "Sm element's id is %s" sm-element-id)
    (when-let ((_ should-import)
               (element-info (org-sm-apiclient-element-info))
               (_ (not (org-sm-apiclient-dismissed-p element-info)))
               (org-capture-templates
                (mapcar (lambda (template)
                          (append template
                                  (list :sm-import-element-info element-info)
                                  (when itemp (list :immediate-finish t :sm-import-item t))
                                  (when sm-element-id (list :sm-import-id sm-element-id))))
                        org-capture-templates)))
      (org-capture nil "s"))
    (if (and itemp (not should-import))
        (progn
          (org-sm-node-show-at-current) ;; Show all first
          (org-sm-node-current-element-present-as-hidden-cloze-text sm-element-id))
      (org-sm-node-show-at-current))))

(defun org-sm-maybe-capture-buffer-finalize ()
  "If buffer at point is a capture buffer, finalize it."
  (when (string-prefix-p "CAPTURE" (buffer-name (current-buffer))) (org-capture-finalize)))

(defun org-sm-node-show-at-current ()
  "Show and unfold the current org entry fully, and if the node is an item element, then hide the subtree headings"
  (outline-show-all))
  ;TODO(let ((itemp (equal ":item" (org-entry-get (point) "SM_ELEMENT_TYPE"))))
  ;  (if itemp
  ;      (outline-show-entry)
  ;    (outline-show-all))))

(defun org-sm-goto-parent ()
  "Go to the parent element based on the 'SM_EXTRACT_PARENT_POINT' and 'SM_EXTRACT_PARENT_ID' properties."
  (interactive)
  (when-let ((id (org-sm-id-at-point)))  ;; Get the ID of the element at the point with the 'drill' tag
    (let ((parent-id (org-entry-get (org-id-find id t) "SM_EXTRACT_PARENT_ID"))
          (parent-point (org-entry-get (org-id-find id t) "SM_EXTRACT_PARENT_POINT")))
      (when parent-id
        (widen)
        (org-sm-id-goto parent-id)  ;; Go to the parent element by ID
        (when parent-point
          (let ((heading-pos (org-get-heading-position parent-id)))
            ;; Move to the relative position stored in 'SM_EXTRACT_PARENT_POINT'
            (goto-char (+ heading-pos (string-to-number parent-point)))))))))

; nocheckin remove
;;(defun org-sm-goto-parent ()
;;  (interactive)
;;  (let ((parent-point (org-entry-get (point) "SM_EXTRACT_PARENT_POINT")))
;;    (when-let ((id (org-entry-get (point) "SM_EXTRACT_PARENT_ID")))
;;      (widen)
;;      (org-sm-id-goto id)
;;      ;; Now go to the parent-point -- nocheckin
;;      (when parent-point
;;          (let ((heading-pos (org-get-heading-position id)))
;;            (goto-char (+ heading-pos (string-to-number parent-point))))))))


(org-link-set-parameters
 "cloze"
 :follow (lambda (path) (message "You clicked me."))
 :export (lambda (path desc backend) (message "TODO You exported me."))
 :face '(:foreground "yellow")
 :help-echo "Click me for a message."
 :display 'org-sm-link)

(add-hook 'org-after-todo-state-change-hook 'org-sm-node-maybe-dismiss-at-point 'append)
(advice-add 'org-sm-goto-next :before #'org-sm-maybe-capture-buffer-finalize)
(advice-add 'org-sm-node-goto-element-id-or-smimport :after #'org-narrow-to-subtree)
(advice-add 'org-sm-node-extract :after #'outline-show-all)
(advice-add 'org-sm-read-point-goto :before #'org-sm-unhide-text)
;NEXT Make an advice that clocks in next supermemo elements
;NEXT Make an advice that clocks in current supermemo elements
;NEXT Make an advice that stops org-sm from clocking if it's during work hours
;TODO Display also the supermemo references
;TODO Make the highlights local file info stuff hide in the PROPERTIES

;TODO If the supermemo element has an id but there's no corresponding org-id, ask user if they want to delete the supermemo element

;TODO (defun org-sm-node-import-current-element ()
;  "Import current element information from SM."
;TODO I need to make it obvious when an element has been imported or not, I need a hot key for "go to the current element in supermemo" that opens the import dialog if not found

;TODO Make an export function:
;TODO Make a function that takes an org entry/element at point and then adds it to supermemo and then adds the :drill: tag and stuff
;TODO It will open a completion dialog with the element types (currently just topic and item)
; This will be useful for when I'm in org capture or whatever, and I just made a note or something, and I want it tracked in supermemo

;TODO (defun org-sm-node-import-current-element ()
;  "Import current element information from SM."
; We need a function that will get run when org-sm doesn't detect that the element has an ID marked into it, or it doesn't detect an org-id matching it, it will ask the user if they would like to import it into emacs, this will open up a capture template, and that's what I'll work on next
;TODO Make sure when you go-to element, you check that the element is dismissed or not

;(global-set-key (kbd "C-c x") 'org-sm-node-extract)
;(global-set-key (kbd "C-c s") 'org-sm-goto-current)
;(global-set-key (kbd "C-c S") 'org-sm-goto-next)
;(global-set-key (kbd "C-c X") 'org-sm-node-convert-and-export-at-point-at-point) ; TODO this is just fo rtesting, change key sym later

(define-key evil-visual-state-map (kbd "SPC s x") 'org-sm-node-extract)
(define-key evil-visual-state-map (kbd "C-c s x") 'org-sm-node-extract)
(define-key evil-visual-state-map (kbd "SPC s z") 'org-sm-node-generate-cloze)
(define-key evil-visual-state-map (kbd "C-c s z") 'org-sm-node-generate-cloze)
(define-key evil-normal-state-map (kbd "SPC s c") 'org-sm-goto-current)
(define-key evil-normal-state-map (kbd "C-c s c") 'org-sm-goto-current)
(define-key evil-normal-state-map (kbd "SPC s e") 'org-sm-node-export-at-point-interactive)
(define-key evil-normal-state-map (kbd "C-c s e") 'org-sm-node-export-at-point-interactive)
(define-key evil-normal-state-map (kbd "SPC s g") 'org-sm-read-point-goto)
(define-key evil-normal-state-map (kbd "C-c s g") 'org-sm-read-point-goto)
(define-key evil-normal-state-map (kbd "SPC g") 'org-sm-read-point-goto)
(define-key evil-normal-state-map (kbd "C-c g") 'org-sm-read-point-goto)
(define-key evil-normal-state-map (kbd "SPC s m") 'org-sm-read-point-set)
(define-key evil-normal-state-map (kbd "C-c s m") 'org-sm-read-point-set)
(define-key evil-normal-state-map (kbd "C-c m") 'org-sm-read-point-set)
(define-key evil-normal-state-map (kbd "SPC m") 'org-sm-read-point-set)
; TODO nocheckin instead of this we're going to overload the =C-c s e= command
;(define-key evil-normal-state-map (kbd "SPC s p") 'org-sm-node-set-priority-at-point)
;(define-key evil-normal-state-map (kbd "C-c s p") 'org-sm-node-set-priority-at-point)
(define-key evil-normal-state-map (kbd "SPC s h") 'org-sm-unhide-text-interactive)
(define-key evil-normal-state-map (kbd "C-c s h") 'org-sm-unhide-text-interactive)
(define-key evil-normal-state-map (kbd "SPC s p") 'org-sm-goto-parent)
(define-key evil-normal-state-map (kbd "C-c s p") 'org-sm-goto-parent)

(define-key evil-normal-state-map (kbd "SPC s r") 'org-sm-node-postpone)
(define-key evil-normal-state-map (kbd "C-c s r") 'org-sm-node-postpone)
(define-key evil-normal-state-map (kbd "SPC s n") 'org-sm-goto-next)
(define-key evil-normal-state-map (kbd "C-c s n") 'org-sm-goto-next)
(define-key evil-normal-state-map (kbd "SPC n") 'org-sm-goto-next)
(define-key evil-normal-state-map (kbd "C-c n") 'org-sm-goto-next)

(define-key evil-visual-state-map (kbd "C-x C-x") 'org-sm-node-extract)

;(defun org-sm-quick-grade (grade)
;  (when (and (equal org-sm-node-current-id (org-roam-id-at-point))
;             (not (org-sm-apiclient-graded-p))
;             (org-sm-apiclient-ready-to-grade-p)
;             (org-sm-apiclient-item-p))
;    (org-sm-node-answer grade)))
;
;(defun org-sm-quick-grade-1 () (org-sm-quick-grade 1))
;(defun org-sm-quick-grade-2 () (org-sm-quick-grade 2))
;(defun org-sm-quick-grade-3 () (org-sm-quick-grade 3))
;(defun org-sm-quick-grade-4 () (org-sm-quick-grade 4))
;(defun org-sm-quick-grade-5 () (org-sm-quick-grade 5))
